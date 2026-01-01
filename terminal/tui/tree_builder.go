package tui

// TreeBuilder helps construct flattened visible node list from hierarchical data
type TreeBuilder struct {
	nodes     []TreeNode
	expansion *TreeExpansion
}

// NewTreeBuilder creates a builder with expansion state
func NewTreeBuilder(expansion *TreeExpansion) *TreeBuilder {
	return &TreeBuilder{
		expansion: expansion,
	}
}

// Reset clears accumulated nodes
func (b *TreeBuilder) Reset() {
	b.nodes = b.nodes[:0]
}

// Add adds a node if visible (parent expanded)
// parentExpanded should be true for root-level nodes
func (b *TreeBuilder) Add(node TreeNode, parentExpanded bool) {
	if !parentExpanded {
		return
	}
	node.Expanded = b.expansion.IsExpanded(node.Key)
	b.nodes = append(b.nodes, node)
}

// Nodes returns accumulated visible nodes
func (b *TreeBuilder) Nodes() []TreeNode {
	return b.nodes
}

// MarkLastSiblings sets IsLast flag on nodes that are last at their depth
// Call after all nodes added, before rendering
func (b *TreeBuilder) MarkLastSiblings() {
	n := len(b.nodes)
	if n == 0 {
		return
	}

	// Track last seen index at each depth
	depthLast := make(map[int]int)

	for i := 0; i < n; i++ {
		depth := b.nodes[i].Depth
		depthLast[depth] = i

		// When we see a node, all deeper nodes before it are "last" at their levels
		// Actually we need to mark when depth decreases or changes
	}

	// Simpler approach: scan backwards, mark last at each depth transition
	for i := n - 1; i >= 0; i-- {
		depth := b.nodes[i].Depth

		// Check if next node (i+1) is at same or lesser depth
		if i == n-1 {
			b.nodes[i].IsLast = true
		} else {
			nextDepth := b.nodes[i+1].Depth
			b.nodes[i].IsLast = nextDepth <= depth
		}
	}
}