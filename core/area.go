package core

// Area represents a rectangular target region
type Area struct {
	X, Y          int // Top-left corner
	Width, Height int // Dimensions (minimum 1x1)
}