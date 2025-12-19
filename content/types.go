package content

// CodeBlock represents a logical group of related lines for spawning
type CodeBlock struct {
	Lines       []string
	IndentLevel int
	HasBraces   bool
}

// PreparedContent holds a batch of processed content blocks
// Generation increments on each refresh to allow consumers to detect swaps
type PreparedContent struct {
	Blocks     []CodeBlock
	Generation int64
}