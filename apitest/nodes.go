package apitest

import (
	"slices"
	"sync"

	"github.com/SpliceFS/common/wire"
)

// NodeList is an in-memory api.NodeLister. The zero value is an empty list. A test sets
// the list after it starts the nodes, because only then are their addresses known. It is
// safe for concurrent use.
type NodeList struct {
	mu    sync.Mutex
	nodes wire.Nodes
}

// Set replaces the list with a copy of nodes.
func (l *NodeList) Set(nodes wire.Nodes) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nodes = wire.Nodes{Cluster: nodes.Cluster, Nodes: slices.Clone(nodes.Nodes)}
}

// Nodes returns a copy of the list.
func (l *NodeList) Nodes() (wire.Nodes, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return wire.Nodes{Cluster: l.nodes.Cluster, Nodes: slices.Clone(l.nodes.Nodes)}, nil
}
