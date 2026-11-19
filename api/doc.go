// Package api implements the HTTP API of the SpliceFS server, version 1: health, login,
// the node list, directory listings, file contents and free space. It depends on small
// interfaces for shares, logins and the node list, so it knows nothing about how they
// are stored. It also holds the types and errors those interfaces speak in, so the
// server and any test double share one definition.
package api
