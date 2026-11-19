package api

import (
	"errors"
	"time"
)

var (
	// ErrNotFound means the name is missing, hidden, outside the share or invalid.
	ErrNotFound = errors.New("not found")
	// ErrNotDir means List was called on something that is not a directory.
	ErrNotDir = errors.New("not a directory")
	// ErrIsDir means Open or Digests was called on a directory.
	ErrIsDir = errors.New("is a directory")
)

// Info describes a file or directory in a share.
type Info struct {
	// Name is the base name. It is "." for the share root.
	Name string
	// Dir is true for directories.
	Dir bool
	// Size is the size in bytes, 0 for directories.
	Size int64
	// MTime is the modification time, truncated to the share's precision, in UTC.
	MTime time.Time
	// ETag is the quoted strong tag of a file, "" for directories.
	ETag string
}

// FileDigests holds the head digest and the tail digest of one version of a file: the
// hex SHA-256 of its first wire.HeadDigestBytes bytes and of its last
// wire.TailDigestBytes bytes.
type FileDigests struct {
	Head string
	Tail string
}
