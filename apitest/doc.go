// Package apitest runs the real API handler of package api over loopback TCP, with
// in-memory doubles behind it: a Share, an Authenticator with an injected clock and a
// NodeLister. Tests of servers and clients use it to talk to a node that answers exactly
// as a server does, because it is the server's own handler. A node can be stopped and
// restarted on the same port, and a file can be cut off in the middle of a response, so
// failures happen on a signal from the test, never after a delay.
package apitest
