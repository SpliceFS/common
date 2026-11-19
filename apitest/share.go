package apitest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/wire"
)

// ErrBadName means Replace was given a name that no request can reach, or one that
// would turn a directory into a file or a file into a directory.
var ErrBadName = errors.New("apitest: bad file name")

// noCut marks a file that is served in full.
const noCut = -1

// Share is an in-memory api.Share. Directories exist because files are in them; the
// root always exists. It is safe for concurrent use, so a test can change a file while
// a node serves it.
type Share struct {
	mu    sync.Mutex
	files map[string]storedFile
	dirs  map[string]time.Time
	space wire.Space
}

// storedFile is one version of a file. Its content is never changed after it is stored.
type storedFile struct {
	content []byte
	mtime   time.Time
	digests api.FileDigests
	cut     int64
}

// NewShare returns an empty share whose root directory has the modification time mtime.
func NewShare(mtime time.Time) *Share {
	return &Share{files: map[string]storedFile{}, dirs: map[string]time.Time{"": mtime}}
}

// Replace stores a copy of content as the file name, a slash-separated path such as
// "a/b.txt", with the modification time mtime. Missing directories above it are made
// with the same time. A file that is replaced gets a new tag when its size or time
// changes, the way a real share tags files, so a block request for the old version is
// answered 412.
func (s *Share) Replace(name string, content []byte, mtime time.Time) error {
	if !validName(name) {
		return fmt.Errorf("%w %q", ErrBadName, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isDir := s.dirs[name]; isDir {
		return fmt.Errorf("%w %q: it is a directory", ErrBadName, name)
	}
	parents := parentsOf(name)
	for _, parent := range parents {
		if _, isFile := s.files[parent]; isFile {
			return fmt.Errorf("%w %q: %q is a file", ErrBadName, name, parent)
		}
	}
	for _, parent := range parents {
		if _, exists := s.dirs[parent]; !exists {
			s.dirs[parent] = mtime
		}
	}
	stored := bytes.Clone(content)
	s.files[name] = storedFile{content: stored, mtime: mtime, digests: digestsOf(stored), cut: noCut}
	return nil
}

// Cut makes the node serving the file name drop the connection once the first after
// bytes of the file are sent, the way a node that goes away in the middle of a read
// does. It lasts until the file is replaced. An unknown name is ignored.
func (s *Share) Cut(name string, after int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.files[name]; ok {
		stored.cut = after
		s.files[name] = stored
	}
}

// SetSpace sets what Space reports.
func (s *Share) SetSpace(space wire.Space) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.space = space
}

// Stat describes the file or directory name; "" is the root.
func (s *Share) Stat(name string) (api.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mtime, ok := s.dirs[name]; ok {
		return dirInfo(name, mtime), nil
	}
	stored, ok := s.files[name]
	if !ok {
		return api.Info{}, api.ErrNotFound
	}
	return fileInfo(name, stored), nil
}

// List returns the entries of the directory name, sorted by name.
func (s *Share) List(name string) ([]api.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFile := s.files[name]; isFile {
		return nil, api.ErrNotDir
	}
	if _, isDir := s.dirs[name]; !isDir {
		return nil, api.ErrNotFound
	}
	var infos []api.Info
	for dir, mtime := range s.dirs {
		if dir != "" && parentOf(dir) == name {
			infos = append(infos, dirInfo(dir, mtime))
		}
	}
	for file, stored := range s.files {
		if parentOf(file) == name {
			infos = append(infos, fileInfo(file, stored))
		}
	}
	slices.SortFunc(infos, func(a, b api.Info) int { return strings.Compare(a.Name, b.Name) })
	return infos, nil
}

// Open opens the file name.
//
//nolint:ireturn // api.Share.Open returns the File interface, so its implementations must too.
func (s *Share) Open(name string) (api.File, error) {
	stored, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	return &openFile{reader: bytes.NewReader(stored.content), info: fileInfo(name, stored), cut: stored.cut}, nil
}

// Digests returns the head and tail digests and the description of the file name.
func (s *Share) Digests(name string) (api.FileDigests, api.Info, error) {
	stored, err := s.lookup(name)
	if err != nil {
		return api.FileDigests{}, api.Info{}, err
	}
	return stored.digests, fileInfo(name, stored), nil
}

// Space returns what SetSpace set.
func (s *Share) Space() (wire.Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.space, nil
}

// lookup returns the file name, or the error the api expects for a directory or a
// missing name.
func (s *Share) lookup(name string) (storedFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isDir := s.dirs[name]; isDir {
		return storedFile{}, api.ErrIsDir
	}
	stored, ok := s.files[name]
	if !ok {
		return storedFile{}, api.ErrNotFound
	}
	return stored, nil
}

// validName reports whether name is a path a request can name: non-empty segments,
// none of them "." or "..".
func validName(name string) bool {
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// parentOf returns the directory that holds name; "" is the root.
func parentOf(name string) string {
	dir := path.Dir(name)
	if dir == "." {
		return ""
	}
	return dir
}

// parentsOf returns every directory above name, root excluded, nearest last.
func parentsOf(name string) []string {
	var parents []string
	for dir := parentOf(name); dir != ""; dir = parentOf(dir) {
		parents = append(parents, dir)
	}
	slices.Reverse(parents)
	return parents
}

func dirInfo(name string, mtime time.Time) api.Info {
	base := "."
	if name != "" {
		base = path.Base(name)
	}
	return api.Info{Name: base, Dir: true, MTime: mtime}
}

func fileInfo(name string, stored storedFile) api.Info {
	size := int64(len(stored.content))
	return api.Info{Name: path.Base(name), Size: size, MTime: stored.mtime, ETag: api.ETag(size, stored.mtime)}
}

func digestsOf(content []byte) api.FileDigests {
	head := sha256.Sum256(content[:min(len(content), wire.HeadDigestBytes)])
	tail := sha256.Sum256(content[len(content)-min(len(content), wire.TailDigestBytes):])
	return api.FileDigests{Head: hex.EncodeToString(head[:]), Tail: hex.EncodeToString(tail[:])}
}

// openFile is an open file of a Share.
type openFile struct {
	reader *bytes.Reader
	info   api.Info
	cut    int64
}

// Read reads from the file. Past the cut it aborts the response, which makes the
// server drop the connection without finishing the body.
func (f *openFile) Read(buf []byte) (int, error) {
	if f.cut == noCut {
		return f.readUncut(buf)
	}
	pos := f.reader.Size() - int64(f.reader.Len())
	if pos >= f.cut {
		panic(http.ErrAbortHandler)
	}
	return f.readUncut(buf[:min(int64(len(buf)), f.cut-pos)])
}

// Seek sets the offset of the next Read.
func (f *openFile) Seek(offset int64, whence int) (int64, error) {
	pos, err := f.reader.Seek(offset, whence)
	if err != nil {
		return pos, fmt.Errorf("apitest: seek %s: %w", f.info.Name, err)
	}
	return pos, nil
}

// Close does nothing: the content stays in memory.
func (*openFile) Close() error { return nil }

// Info describes the version of the file that was opened.
func (f *openFile) Info() api.Info { return f.info }

func (f *openFile) readUncut(buf []byte) (int, error) {
	//nolint:wrapcheck // The only error is io.EOF, which readers of a File compare by identity.
	return f.reader.Read(buf)
}
