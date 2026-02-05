package maze

import (
	"math/rand"
	"time"
)

// Cell types
const (
	Wall    = true
	Passage = false
)

type Point struct {
	X, Y int
}

type Config struct {
	Width, Height int

	// Braiding: 0.0 (Perfect Maze/Tree) to 1.0 (No dead ends/Graph).
	// Higher values add cycles. Constraints (No Plazas/Pillars) take precedence.
	Braiding float64

	// If true, the outer boundary is set to Passage.
	RemoveBorders bool

	StartPos *Point // Optional (nil = Automatic)
	EndPos   *Point // Optional (nil = Automatic)
	Seed     int64  // Optional (0 = Random)
}

type Result struct {
	Grid         [][]bool
	Start, End   Point
	SolutionPath []Point
}

// Generate creates a stochastic topological maze.
func Generate(cfg Config) Result {
	// 1. Setup Topology
	// We round DOWN to the nearest odd number to stay within requested bounds.
	rows := ensureOdd(cfg.Height)
	cols := ensureOdd(cfg.Width)

	// 2. Initialize Grid (Filled with Walls)
	grid := make([][]bool, rows)
	for i := range grid {
		grid[i] = make([]bool, cols)
		for j := range grid[i] {
			grid[i][j] = Wall
		}
	}

	// 3. RNG Setup
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// 4. Resolve Start/End Logic
	startDefX, startDefY := 1, 1
	endDefX, endDefY := cols-2, rows-2

	if cfg.RemoveBorders {
		// Jailbreak: Start Center, End Right Edge
		startDefX, startDefY = (cols/2)|1, (rows/2)|1
		endDefX, endDefY = cols-1, (rows/2)|1
	}

	start := resolvePoint(rows, cols, cfg.StartPos, startDefX, startDefY)
	end := resolvePoint(rows, cols, cfg.EndPos, endDefX, endDefY)

	// 5. Core Generation (Recursive Backtracker)
	// Generates a Uniform Spanning Tree (UST).
	recursiveBacktracker(grid, start, rng)

	// 6. Mode: Remove Borders (Jailbreak)
	// Must be done BEFORE braiding so the braiding algo detects external connections
	// and doesn't try to force internal loops for edge nodes.
	if cfg.RemoveBorders {
		stripBorders(grid)
	}

	// 7. Apply Braiding (Homological Complexity)
	// Introduces cycles while preventing Plazas and Pillars.
	if cfg.Braiding > 0 {
		applySmartBraiding(grid, cfg.Braiding, rng)
	}

	// 8. Final Connectivity Enforcement
	if cfg.RemoveBorders {
		// Ensure Start/End are open if they land on the cleared border
		grid[start.Y][start.X] = Passage
		grid[end.Y][end.X] = Passage
	} else {
		// Standard Mode: Ensure Start/End are walkable
		forceOpen(grid, start)
		forceOpen(grid, end)
	}

	// 9. Calculate Solution Path (BFS)
	path := solveBFS(grid, start, end)

	return Result{
		Grid:         grid,
		Start:        start,
		End:          end,
		SolutionPath: path,
	}
}

// --- Core Algorithms ---

func recursiveBacktracker(grid [][]bool, start Point, rng *rand.Rand) {
	rows, cols := len(grid), len(grid[0])

	// Ensure start is within bounds before beginning
	if start.X < 0 || start.X >= cols || start.Y < 0 || start.Y >= rows {
		start = Point{1, 1}
	}

	stack := []Point{start}
	grid[start.Y][start.X] = Passage

	dirs := []Point{{0, -2}, {0, 2}, {-2, 0}, {2, 0}}

	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		candidates := make([]Point, 0, 4)

		for _, d := range dirs {
			nx, ny := curr.X+d.X, curr.Y+d.Y
			// Check Bounds (Leave 1 cell border for walls)
			if nx > 0 && nx < cols-1 && ny > 0 && ny < rows-1 {
				if grid[ny][nx] == Wall {
					candidates = append(candidates, d)
				}
			}
		}

		if len(candidates) > 0 {
			d := candidates[rng.Intn(len(candidates))]
			wallX, wallY := curr.X+d.X/2, curr.Y+d.Y/2
			nextX, nextY := curr.X+d.X, curr.Y+d.Y

			grid[wallY][wallX] = Passage
			grid[nextY][nextX] = Passage

			stack = append(stack, Point{nextX, nextY})
		} else {
			stack = stack[:len(stack)-1]
		}
	}
}

func applySmartBraiding(grid [][]bool, probability float64, rng *rand.Rand) {
	rows, cols := len(grid), len(grid[0])

	// Iterate over odd nodes (Rooms)
	for y := 1; y < rows-1; y += 2 {
		for x := 1; x < cols-1; x += 2 {
			if grid[y][x] == Wall {
				continue
			}

			// 1. Identify Dead End
			// A node is a dead end if it has exactly 1 Passage neighbor.
			exits := 0
			checkDirs := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
			for _, d := range checkDirs {
				// Bounds check included for safety, though loop ignores edges
				if grid[y+d.Y][x+d.X] == Passage {
					exits++
				}
			}

			if exits == 1 && rng.Float64() < probability {
				// 2. Find valid walls to remove to create a loop
				candidates := make([]Point, 0, 4)

				// Look at orthogonal neighbors (distance 2)
				jumpDirs := []Point{{0, -2}, {0, 2}, {-2, 0}, {2, 0}}
				for _, jd := range jumpDirs {
					nx, ny := x+jd.X, y+jd.Y     // Target Neighbor
					wx, wy := x+jd.X/2, y+jd.Y/2 // The intervening Wall

					if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
						// Connect if neighbor is Passage and the wall is currently blocking
						if grid[ny][nx] == Passage && grid[wy][wx] == Wall {
							// 3. TOPOLOGY CHECK: Plazas & Pillars
							if canSafelyRemoveWall(grid, wx, wy) {
								candidates = append(candidates, Point{wx, wy})
							}
						}
					}
				}

				if len(candidates) > 0 {
					c := candidates[rng.Intn(len(candidates))]
					grid[c.Y][c.X] = Passage
				}
			}
		}
	}
}

// canSafelyRemoveWall checks if removing grid[y][x] creates prohibited topology:
// 1. Plazas (2x2 Passages).
// 2. Pillars (Isolated Walls).
func canSafelyRemoveWall(grid [][]bool, x, y int) bool {
	rows, cols := len(grid), len(grid[0])

	// Helper for bounds-safe read
	isP := func(tx, ty int) bool {
		if tx < 0 || tx >= cols || ty < 0 || ty >= rows {
			return false // Treat out of bounds as Wall for plaza checking purposes
		}
		return grid[ty][tx] == Passage
	}

	// --- Check 1: No Plazas (2x2 Open Space) ---
	// If we turn (x,y) to passage, check the 4 quadrants around it.

	// Top-Left quadrant containing (x,y)
	if isP(x-1, y-1) && isP(x, y-1) && isP(x-1, y) {
		return false
	}
	// Top-Right quadrant
	if isP(x, y-1) && isP(x+1, y-1) && isP(x+1, y) {
		return false
	}
	// Bottom-Left quadrant
	if isP(x-1, y) && isP(x-1, y+1) && isP(x, y+1) {
		return false
	}
	// Bottom-Right quadrant
	if isP(x+1, y) && isP(x, y+1) && isP(x+1, y+1) {
		return false
	}

	// --- Check 2: No Pillars (Isolated Walls) ---
	// Removing this wall might isolate an adjacent wall node.
	// We must check the orthogonal neighbors of (x,y).
	// If a neighbor is a Wall, ensure it has at least one other Wall connection.

	ortho := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	for _, d := range ortho {
		nx, ny := x+d.X, y+d.Y
		// Check bounds
		if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
			if grid[ny][nx] == Wall {
				// Check if this wall (nx, ny) would become isolated.
				// It is isolated if ALL its neighbors are Passages.
				// Note: (x,y) is currently Wall in memory, but conceptually Passage for this check.

				wallConnections := 0
				for _, d2 := range ortho {
					nnx, nny := nx+d2.X, ny+d2.Y
					// If the neighbor is (x,y), it's GOING to be a passage, so don't count it as a wall connection.
					if nnx == x && nny == y {
						continue
					}

					if nnx >= 0 && nnx < cols && nny >= 0 && nny < rows {
						if grid[nny][nnx] == Wall {
							wallConnections++
						}
					}
				}

				if wallConnections == 0 {
					return false // Removing (x,y) creates a pillar at (nx, ny)
				}
			}
		}
	}

	return true
}

func stripBorders(grid [][]bool) {
	rows, cols := len(grid), len(grid[0])
	// Set Top/Bottom rows to Passage
	for x := 0; x < cols; x++ {
		grid[0][x] = Passage
		grid[rows-1][x] = Passage
	}
	// Set Left/Right cols to Passage
	for y := 0; y < rows; y++ {
		grid[y][0] = Passage
		grid[y][cols-1] = Passage
	}
}

// --- Helpers ---

func ensureOdd(n int) int {
	if n < 3 {
		return 3
	}
	if n%2 == 0 {
		return n - 1 // Round down to stay within bounds
	}
	return n
}

func resolvePoint(h, w int, p *Point, defX, defY int) Point {
	if p == nil {
		return Point{defX, defY}
	}
	x, y := p.X, p.Y
	// Clamp
	if x < 0 {
		x = 0
	}
	if x >= w {
		x = w - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= h {
		y = h - 1
	}
	return Point{x, y}
}

func forceOpen(grid [][]bool, p Point) {
	if p.X < 0 || p.Y < 0 || p.Y >= len(grid) || p.X >= len(grid[0]) {
		return
	}
	grid[p.Y][p.X] = Passage

	// Simple check: if isolated, connect to nearest neighbor
	dirs := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	hasPassage := false
	for _, d := range dirs {
		nx, ny := p.X+d.X, p.Y+d.Y
		if nx >= 0 && nx < len(grid[0]) && ny >= 0 && ny < len(grid) {
			if grid[ny][nx] == Passage {
				hasPassage = true
				break
			}
		}
	}

	if !hasPassage {
		for _, d := range dirs {
			nx, ny := p.X+d.X, p.Y+d.Y
			if nx > 0 && nx < len(grid[0])-1 && ny > 0 && ny < len(grid)-1 {
				grid[ny][nx] = Passage
				return
			}
		}
	}
}

func solveBFS(grid [][]bool, start, end Point) []Point {
	// Quick validity check
	if start.X < 0 || start.Y < 0 || end.X < 0 || end.Y < 0 {
		return nil
	}
	rows, cols := len(grid), len(grid[0])
	if start.Y >= rows || start.X >= cols || end.Y >= rows || end.X >= cols {
		return nil
	}

	if grid[start.Y][start.X] == Wall || grid[end.Y][end.X] == Wall {
		return nil
	}

	queue := []Point{start}
	cameFrom := make(map[Point]Point)
	visited := make(map[Point]bool)
	visited[start] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr == end {
			// Reconstruct Path
			path := []Point{}
			for curr != start {
				path = append([]Point{curr}, path...)
				curr = cameFrom[curr]
			}
			path = append([]Point{start}, path...)
			return path
		}

		dirs := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
		for _, d := range dirs {
			nx, ny := curr.X+d.X, curr.Y+d.Y
			next := Point{nx, ny}

			if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
				if grid[ny][nx] == Passage && !visited[next] {
					visited[next] = true
					cameFrom[next] = curr
					queue = append(queue, next)
				}
			}
		}
	}
	return nil
}