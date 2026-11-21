// Package remote is a thin, stateless HTTP client for one SpliceFS server node, speaking
// version 1 of the API that package api serves.
//
// A Node holds only the node's name, its base URL and an injected *http.Client. It does not
// log in on its own, retry, cache or know about other nodes; those concerns belong to the
// layers above. Every method takes the context and the bearer token to use, and a Node is
// safe for concurrent use.
//
// The caller configures the *http.Client. TLSConfig gives it the TLS part: the standard
// verification, or, for a client given the public key of the cluster, that one plus
// trust in every server whose certificate the cluster key signed. TLSDialer does the same
// for a transport that serves many hosts of one cluster, by building the configuration for
// each host it dials.
//
// Failures are reported as one of the exported sentinel errors, wrapped with the node name
// and the operation, so callers branch with errors.Is. When the caller's own context ends,
// the context's error is returned instead, so "I gave up" can be told from "the node is
// down".
package remote
