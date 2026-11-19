package apitest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/wire"
)

func testTime() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 5, time.UTC) }

func hexSum(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// newTestShare is a share with a file at the root and one in a subdirectory.
func newTestShare(t *testing.T) *Share {
	t.Helper()
	share := NewShare(testTime())
	for name, content := range map[string]string{"a.txt": "alpha", "sub/b.bin": "beta"} {
		if err := share.Replace(name, []byte(content), testTime()); err != nil {
			t.Fatalf("Replace %s: %v", name, err)
		}
	}
	return share
}

func TestShareStat(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	tests := []struct {
		name string
		want api.Info
		err  error
	}{
		{name: "", want: api.Info{Name: ".", Dir: true, MTime: testTime()}},
		{name: "sub", want: api.Info{Name: "sub", Dir: true, MTime: testTime()}},
		{name: "sub/b.bin", want: api.Info{Name: "b.bin", Size: 4, MTime: testTime(), ETag: `"4-6ae729c0-5"`}},
		{name: "missing", err: api.ErrNotFound},
	}
	for _, tt := range tests {
		got, err := share.Stat(tt.name)
		if !errors.Is(err, tt.err) || got != tt.want {
			t.Errorf("Stat(%q) = %+v, %v; want %+v, %v", tt.name, got, err, tt.want, tt.err)
		}
	}
}

func TestShareList(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	tests := []struct {
		name  string
		names []string
		err   error
	}{
		{name: "", names: []string{"a.txt", "sub"}},
		{name: "sub", names: []string{"b.bin"}},
		{name: "a.txt", err: api.ErrNotDir},
		{name: "missing", err: api.ErrNotFound},
	}
	for _, tt := range tests {
		infos, err := share.List(tt.name)
		if !errors.Is(err, tt.err) {
			t.Errorf("List(%q) error = %v, want %v", tt.name, err, tt.err)
		}
		names := make([]string, 0, len(infos))
		for _, info := range infos {
			names = append(names, info.Name)
		}
		if strings.Join(names, ",") != strings.Join(tt.names, ",") {
			t.Errorf("List(%q) = %v, want %v", tt.name, names, tt.names)
		}
	}
}

func TestShareOpenAndDigests(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	file, err := share.Open("a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "alpha" {
		t.Errorf("content = %q, %v; want alpha", data, err)
	}
	if err = file.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	digests, info, err := share.Digests("a.txt")
	if err != nil {
		t.Fatalf("Digests: %v", err)
	}
	want := api.FileDigests{Head: hexSum("alpha"), Tail: hexSum("alpha")}
	if digests != want || info != file.Info() {
		t.Errorf("Digests = %+v, %+v; want %+v, %+v", digests, info, want, file.Info())
	}
}

func TestShareRefusesToOpenWhatIsNoFile(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	for name, wantErr := range map[string]error{"sub": api.ErrIsDir, "missing": api.ErrNotFound} {
		var err error
		if _, err = share.Open(name); !errors.Is(err, wantErr) {
			t.Errorf("Open(%q) = %v, want %v", name, err, wantErr)
		}
		if _, _, err = share.Digests(name); !errors.Is(err, wantErr) {
			t.Errorf("Digests(%q) = %v, want %v", name, err, wantErr)
		}
	}
}

func TestDigestsCoverHeadAndTail(t *testing.T) {
	t.Parallel()
	share := NewShare(testTime())
	content := strings.Repeat("h", wire.HeadDigestBytes) + "middle" + strings.Repeat("t", wire.TailDigestBytes)
	if err := share.Replace("big", []byte(content), testTime()); err != nil {
		t.Fatal(err)
	}
	digests, _, err := share.Digests("big")
	if err != nil {
		t.Fatal(err)
	}
	want := api.FileDigests{
		Head: hexSum(strings.Repeat("h", wire.HeadDigestBytes)),
		Tail: hexSum(strings.Repeat("t", wire.TailDigestBytes)),
	}
	if digests != want {
		t.Errorf("digests = %+v, want %+v", digests, want)
	}
}

func TestReplaceChangesTheFile(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	content := []byte("gamma!")
	later := testTime().Add(time.Second)
	if err := share.Replace("a.txt", content, later); err != nil {
		t.Fatal(err)
	}
	content[0] = 'X' // the share keeps its own copy
	info, err := share.Stat("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 6 || !info.MTime.Equal(later) || info.ETag != `"6-6ae729c1-5"` {
		t.Errorf("after Replace: %+v", info)
	}
	file, err := share.Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if data, readErr := io.ReadAll(file); readErr != nil || string(data) != "gamma!" {
		t.Errorf("content = %q, %v; want gamma!", data, readErr)
	}
}

func TestReplaceRefusesBadNames(t *testing.T) {
	t.Parallel()
	share := newTestShare(t)
	for _, name := range []string{"", "/x", "x/", "a/../b", "./x", "sub", "a.txt/x"} {
		if err := share.Replace(name, nil, testTime()); !errors.Is(err, ErrBadName) {
			t.Errorf("Replace(%q) = %v, want ErrBadName", name, err)
		}
	}
}

func TestSpace(t *testing.T) {
	t.Parallel()
	share := NewShare(testTime())
	share.SetSpace(wire.Space{Total: 10, Free: 4})
	if got, err := share.Space(); err != nil || got != (wire.Space{Total: 10, Free: 4}) {
		t.Errorf("Space = %+v, %v", got, err)
	}
}

func TestCutAbortsTheResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		seek  int64
		after int64
		want  string
	}{
		{name: "from the start", seek: 0, after: 3, want: "alp"},
		{name: "within the cut", seek: 1, after: 3, want: "lp"},
		{name: "past the cut", seek: 4, after: 3, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			share := newTestShare(t)
			share.Cut("a.txt", tt.after)
			file, err := share.Open("a.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = file.Seek(tt.seek, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			var got []byte
			reason := readUntilPanic(file, &got)
			if !errors.Is(reason, http.ErrAbortHandler) || string(got) != tt.want {
				t.Errorf("read %q then %v; want %q then ErrAbortHandler", got, reason, tt.want)
			}
		})
	}
}

// readUntilPanic reads r into dst until a read panics, and returns the panic value as
// an error, or nil when reading ended without one.
func readUntilPanic(r io.Reader, dst *[]byte) (reason error) {
	defer func() {
		value := recover()
		if err, ok := value.(error); ok {
			reason = err
		}
	}()
	buf := make([]byte, 2)
	for {
		n, err := r.Read(buf)
		*dst = append(*dst, buf[:n]...)
		if err != nil {
			return nil
		}
	}
}

func TestSeekBeforeTheStartFails(t *testing.T) {
	t.Parallel()
	file, err := newTestShare(t).Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(-1, io.SeekStart); err == nil {
		t.Error("Seek(-1) succeeded")
	}
}
