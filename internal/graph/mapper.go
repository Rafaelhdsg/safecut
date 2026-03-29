package graph

// Node represents a single cloud resource in the dependency graph.
type Node struct {
	ID       string
	Type     string // e.g. "PublicIP", "NIC", "VM", "Disk"
	Name     string
	Children []*Node
	Parent   *Node
}

// DependencyGraph maps relationships between cloud resources
// (e.g., IP -> NIC -> VM -> Disk) to detect orphans and assess impact.
type DependencyGraph struct {
	nodes map[string]*Node
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{nodes: make(map[string]*Node)}
}

func (g *DependencyGraph) AddNode(n *Node) {
	g.nodes[n.ID] = n
}

func (g *DependencyGraph) GetNode(id string) (*Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// Link establishes a parent-child relationship between two resource nodes.
func (g *DependencyGraph) Link(parentID, childID string) bool {
	parent, ok := g.nodes[parentID]
	if !ok {
		return false
	}
	child, ok := g.nodes[childID]
	if !ok {
		return false
	}
	child.Parent = parent
	parent.Children = append(parent.Children, child)
	return true
}

// Orphans returns all nodes that have no parent and no children,
// indicating potentially wasted resources.
func (g *DependencyGraph) Orphans() []*Node {
	var orphans []*Node
	for _, n := range g.nodes {
		if n.Parent == nil && len(n.Children) == 0 {
			orphans = append(orphans, n)
		}
	}
	return orphans
}
