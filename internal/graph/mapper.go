package graph

import "strings"

// Node represents a single cloud resource in the dependency graph.
type Node struct {
	ID       string
	Type     string
	Name     string
	Children []*Node
	Parent   *Node
}

// DependencyGraph maps relationships between cloud resources
// (e.g., IP → NIC → VM → Disk) to detect orphans and assess impact.
type DependencyGraph struct {
	nodes map[string]*Node
	index map[string]*Node // lowercase ID → node, for case-insensitive lookups
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*Node),
		index: make(map[string]*Node),
	}
}

func (g *DependencyGraph) AddNode(n *Node) {
	g.nodes[n.ID] = n
	g.index[strings.ToLower(n.ID)] = n
}

func (g *DependencyGraph) GetNode(id string) (*Node, bool) {
	n, ok := g.nodes[id]
	if ok {
		return n, true
	}
	n, ok = g.index[strings.ToLower(id)]
	return n, ok
}

// Link establishes a parent-child relationship between two resource nodes.
// Uses case-insensitive lookup because Azure returns mixed-case IDs.
func (g *DependencyGraph) Link(parentID, childID string) bool {
	parent, ok := g.GetNode(parentID)
	if !ok {
		return false
	}
	child, ok := g.GetNode(childID)
	if !ok {
		return false
	}
	if child.Parent != nil {
		return false
	}
	child.Parent = parent
	parent.Children = append(parent.Children, child)
	return true
}

// Orphans returns all nodes that have no parent and no children.
func (g *DependencyGraph) Orphans() []*Node {
	var orphans []*Node
	for _, n := range g.nodes {
		if n.Parent == nil && len(n.Children) == 0 {
			orphans = append(orphans, n)
		}
	}
	return orphans
}

// HasDependents returns true if the node has children that depend on it.
func (g *DependencyGraph) HasDependents(id string) bool {
	n, ok := g.GetNode(id)
	if !ok {
		return false
	}
	return len(n.Children) > 0
}

// DependencyChain returns a human-readable chain from root to leaf
// for a given resource (e.g., "VM → NIC → Public IP").
func (g *DependencyGraph) DependencyChain(id string) []string {
	n, ok := g.GetNode(id)
	if !ok {
		return nil
	}

	var chain []string

	cur := n
	for cur.Parent != nil {
		cur = cur.Parent
	}

	var walk func(*Node)
	walk = func(node *Node) {
		chain = append(chain, node.Name+" ("+shortType(node.Type)+")")
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(cur)
	return chain
}

func shortType(t string) string {
	parts := strings.Split(t, "/")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return t
}
