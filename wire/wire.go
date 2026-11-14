// Package wire defines the JSON bodies and header names of the SpliceFS HTTP API,
// version 1. The server and the client both use these types, so a change here is a
// change to the protocol.
package wire

import "time"

// HeaderHeadDigest carries the head digest of a file on HEAD responses: the
// hex-encoded SHA-256 of its first HeadDigestBytes bytes.
const HeaderHeadDigest = "X-SpliceFS-Head-Digest"

// HeadDigestBytes is how much of the start of a file the head digest covers.
const HeadDigestBytes = 64 << 10

// HeaderTailDigest carries the tail digest of a file on HEAD responses: the
// hex-encoded SHA-256 of its last TailDigestBytes bytes, or of the whole file when it is
// shorter. Head and tail together tell two copies apart that have the same size and
// time, in constant time whatever the size of the file.
//
// A client sends both HeaderHeadDigest and HeaderTailDigest on every block (range)
// request, with the values it chose the file by. The server answers 412 Precondition
// Failed when the file no longer has them, so a client never mixes blocks of two
// different copies of a file.
const HeaderTailDigest = "X-SpliceFS-Tail-Digest"

// TailDigestBytes is how much of the end of a file the tail digest covers.
const TailDigestBytes = 64 << 10

// HeaderServerVersion carries the version of the server on every response. A client
// reads it to learn which server it talks to.
//
// Either side ends the exchange when the versions are not compatible: the major
// versions must be equal, and the server minor must be equal to or higher than the
// client minor. A newer server can therefore serve an older client, but not the other
// way round.
const HeaderServerVersion = "X-SpliceFS-Server-Version"

// HeaderClientVersion carries the version of the client on every request. A request
// without it comes from a client older than this rule. The server accepts it, because
// the rule lets an older client minor work with a newer server.
const HeaderClientVersion = "X-SpliceFS-Client-Version"

// Health is the body of GET /v1/health.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Node    string `json:"node,omitempty"`
}

// LoginRequest is the body of POST /v1/login.
type LoginRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// LoginResponse is the answer to a successful login.
type LoginResponse struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

// Node is one member of a cluster.
type Node struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url"  yaml:"url"`
}

// Nodes is the body of GET /v1/nodes and the content of a node list file.
type Nodes struct {
	Cluster string `json:"cluster" yaml:"cluster"`
	Nodes   []Node `json:"nodes"   yaml:"nodes"`
}

// Entry is one name in a directory listing. ETag is empty for directories.
type Entry struct {
	Name  string    `json:"name"`
	Dir   bool      `json:"dir"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
	ETag  string    `json:"etag,omitempty"`
}

// Listing is the body of GET /v1/tree/...
type Listing struct {
	Entries []Entry `json:"entries"`
}

// Space is the body of GET /v1/space/<share>, in bytes.
type Space struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

// Error is the body of every error response.
type Error struct {
	Error string `json:"error"`
}
