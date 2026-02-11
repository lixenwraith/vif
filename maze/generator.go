package maze

import (
	"math"
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

// RoomSpec defines an explicitly positioned room
// CenterX, CenterY specify the room center in grid coordinates
type RoomSpec struct {
	CenterX int
	CenterY int
	Width   int
	Height  int
}

// RoomResult describes a generated room
type RoomResult struct {
	X, Y          int     // Top-left corner
	Width, Height int     // Dimensions
	Entries       []Point // Entry points connecting to maze
}

// resolvedRoom is internal representation with top-left coords
type resolvedRoom struct {
	x, y, w, h int
}

type Config struct {
	Width, Height int

	// Braiding: 0.0 (Perfect Maze/Tree) to 1.0 (No dead ends/Graph)
	// Higher values add cycles. Constraints (No Plazas/Pillars) take precedence.
	Braiding float64

	// If true, the outer boundary is set to Passage
	RemoveBorders bool

	StartPos *Point // Optional (nil = Automatic)
	EndPos   *Point // Optional (nil = Automatic)
	Seed     int64  // Optional (0 = Random)

	// Room generation
	// RoomCount specifies total rooms to generate (0 = no rooms)
	// Rooms slice provides explicit positions for first len(Rooms) rooms
	// Remaining rooms (RoomCount - len(Rooms)) are placed randomly
	RoomCount         int
	Rooms             []RoomSpec // Explicit room specifications
	DefaultRoomWidth  int        // Width for random rooms (0 = 15)
	DefaultRoomHeight int        // Height for random rooms (0 = 11)
}

type Result struct {
	Grid         [][]bool
	Start, End   Point
	SolutionPath []Point
	Rooms        []RoomResult
}

// Generate creates a stochastic topological maze with optional rooms
func Generate(cfg Config) Result {
	rows := ensureOdd(cfg.Height)
	cols := ensureOdd(cfg.Width)

	grid := make([][]bool, rows)
	for i := range grid {
		grid[i] = make([]bool, cols)
		for j := range grid[i] {
			grid[i][j] = Wall
		}
	}

	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// Resolve and reserve rooms before maze generation
	resolved := resolveRooms(cfg, cols, rows, rng)
	reserveRooms(grid, resolved)

	// Resolve Start/End
	startDefX, startDefY := 1, 1
	endDefX, endDefY := cols-2, rows-2

	if cfg.RemoveBorders {
		startDefX, startDefY = (cols/2)|1, (rows/2)|1
		endDefX, endDefY = cols-1, (rows/2)|1
	}

	start := resolvePoint(rows, cols, cfg.StartPos, startDefX, startDefY)
	end := resolvePoint(rows, cols, cfg.EndPos, endDefX, endDefY)

	// Ensure start is not inside a room
	start = adjustStartForRooms(start, resolved, cols, rows)

	recursiveBacktracker(grid, start, rng)

	if cfg.RemoveBorders {
		stripBorders(grid)
	}

	if cfg.Braiding > 0 {
		applySmartBraiding(grid, cfg.Braiding, rng)
	}

	// Connect rooms to maze passages
	roomResults := connectRooms(grid, resolved, rng)

	if cfg.RemoveBorders {
		grid[start.Y][start.X] = Passage
		grid[end.Y][end.X] = Passage
	} else {
		forceOpen(grid, start)
		forceOpen(grid, end)
	}

	path := solveBFS(grid, start, end)

	return Result{
		Grid:         grid,
		Start:        start,
		End:          end,
		SolutionPath: path,
		Rooms:        roomResults,
	}
}

// resolveRooms converts config to concrete room bounds
// Uses explicit specs first, then generates random rooms for remainder
// Dynamic sizing: when defaults not specified, scales inversely with sqrt(roomCount)
func resolveRooms(cfg Config, cols, rows int, rng *rand.Rand) []resolvedRoom {
	if cfg.RoomCount <= 0 {
		return nil
	}

	resolved := make([]resolvedRoom, 0, cfg.RoomCount)

	// Calculate default dimensions based on room count if not specified, improved by scaling and odd-parity enforcement
	defaultW := ensureOdd(cfg.DefaultRoomWidth)
	defaultH := ensureOdd(cfg.DefaultRoomHeight)

	if defaultW <= 0 || defaultH <= 0 {
		divisor := 1.0 + math.Sqrt(float64(cfg.RoomCount))
		if defaultW <= 0 {
			defaultW = ensureOdd(int(float64(cols) / divisor))
		}
		if defaultH <= 0 {
			defaultH = ensureOdd(int(float64(rows) / divisor))
		}
	}

	// Ensure minimums for connectivity
	if defaultW < 3 {
		defaultW = 3
	}
	if defaultH < 3 {
		defaultH = 3
	}

	explicitCount := len(cfg.Rooms)
	if explicitCount > cfg.RoomCount {
		explicitCount = cfg.RoomCount
	}

	for i := 0; i < explicitCount; i++ {
		spec := cfg.Rooms[i]
		w, h := ensureOdd(spec.Width), ensureOdd(spec.Height)
		if w <= 0 {
			w = defaultW
		}
		if h <= 0 {
			h = defaultH
		}

		if spec.CenterX == 0 && spec.CenterY == 0 {
			// Random placement for explicit size
			room, ok := resolveRandomRoom(w, h, cols, rows, resolved, rng)
			if ok {
				resolved = append(resolved, room)
			}
		} else {
			room, ok := resolveExplicitRoom(spec.CenterX, spec.CenterY, w, h, cols, rows, resolved)
			if ok {
				resolved = append(resolved, room)
			}
		}
	}

	// Generate random rooms for remainder
	randomCount := cfg.RoomCount - len(resolved)
	curW, curH := defaultW, defaultH

	for i := 0; i < randomCount; i++ {
		// Adaptive shrinking. If we can't place a room, shrink it and try again.
		placed := false
		for attempt := 0; attempt < 3; attempt++ {
			room, ok := resolveRandomRoom(curW, curH, cols, rows, resolved, rng)
			if ok {
				resolved = append(resolved, room)
				placed = true
				break
			}
			// Shrink and try again
			curW = ensureOdd(curW - 2)
			curH = ensureOdd(curH - 2)
			if curW < 3 || curH < 3 {
				break
			}
		}
		if !placed {
			continue // Skip this room if it simply won't fit
		}
	}

	return resolved
}

// resolveExplicitRoom converts explicit center coordinates and dimensions to resolved bounds
func resolveExplicitRoom(centerX, centerY, width, height, cols, rows int, existing []resolvedRoom) (resolvedRoom, bool) {
	if width <= 0 || height <= 0 {
		return resolvedRoom{}, false
	}

	w := ensureOdd(width)
	h := ensureOdd(height)

	// Calculate top-left such that the resulting range [x, x+w) is centered on centerX/Y, since w/h are odd, (w-1)/2 is the radius
	x := centerX - (w-1)/2
	y := centerY - (h-1)/2

	// Ensure x, y are odd to align with the passage grid (prevents clampRoomBounds from shifting center)
	if x%2 == 0 {
		x-- // Shift left to keep center priority
	}
	if y%2 == 0 {
		y-- // Shift up to keep center priority
	}

	// Clamp to valid bounds and preventing edge placement
	x, y, w, h = clampRoomBounds(x, y, w, h, cols, rows)

	if w < 3 || h < 3 {
		return resolvedRoom{}, false
	}

	room := resolvedRoom{x: x, y: y, w: w, h: h}
	if roomOverlaps(room, existing) {
		return resolvedRoom{}, false
	}

	return room, true
}

// resolveRandomRoom finds a valid random position for a room
func resolveRandomRoom(width, height, cols, rows int, existing []resolvedRoom, rng *rand.Rand) (resolvedRoom, bool) {
	w := ensureOdd(width)
	h := ensureOdd(height)

	// Margin (3) to ensure rooms don't touch borders and align with the recursive backtracker's passage grid (odd indices)
	minX, minY := 3, 3
	maxX := (cols - 3 - w)
	maxY := (rows - 3 - h)

	if maxX < minX || maxY < minY {
		return resolvedRoom{}, false
	}

	const maxAttempts = 100
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Pick odd coordinates to align with maze nodes
		x := minX + rng.Intn((maxX-minX)/2+1)*2
		y := minY + rng.Intn((maxY-minY)/2+1)*2

		room := resolvedRoom{x: x, y: y, w: w, h: h}
		if !roomOverlaps(room, existing) {
			return room, true
		}
	}

	return resolvedRoom{}, false
}

// clampRoomBounds ensures room fits within maze boundaries with a margin
func clampRoomBounds(x, y, w, h, cols, rows int) (int, int, int, int) {
	// Enforce a minimum 2-cell buffer from absolute edges to protect room walls in Jailbreak mode
	margin := 2

	if x < margin {
		x = margin
	}
	if y < margin {
		y = margin
	}

	// Ensure x, y are odd for grid alignment
	if x%2 == 0 {
		x++
	}
	if y%2 == 0 {
		y++
	}

	if x+w > cols-margin {
		w = ensureOdd(cols - margin - x)
	}
	if y+h > rows-margin {
		h = ensureOdd(rows - margin - y)
	}

	return x, y, w, h
}

// roomOverlaps checks if room intersects any existing room (with 3-cell gap)
func roomOverlaps(room resolvedRoom, existing []resolvedRoom) bool {
	const gap = 3
	for _, r := range existing {
		if room.x < r.x+r.w+gap && room.x+room.w+gap > r.x &&
			room.y < r.y+r.h+gap && room.y+room.h+gap > r.y {
			return true
		}
	}
	return false
}

// reserveRooms marks room interiors as Passage before maze generation
func reserveRooms(grid [][]bool, rooms []resolvedRoom) {
	for _, room := range rooms {
		for y := room.y; y < room.y+room.h; y++ {
			for x := room.x; x < room.x+room.w; x++ {
				grid[y][x] = Passage
			}
		}
	}
}

// adjustStartForRooms moves start point outside rooms if necessary
func adjustStartForRooms(start Point, rooms []resolvedRoom, cols, rows int) Point {
	for _, room := range rooms {
		if start.X >= room.x && start.X < room.x+room.w &&
			start.Y >= room.y && start.Y < room.y+room.h {
			// Start is inside room; move to nearest valid odd cell outside
			// Try below room first
			newY := room.y + room.h
			if newY|1 < rows-1 {
				return Point{X: start.X | 1, Y: newY | 1}
			}
			// Try above room
			newY = room.y - 2
			if newY > 0 {
				return Point{X: start.X | 1, Y: newY | 1}
			}
			// Fallback to corner
			return Point{X: 1, Y: 1}
		}
	}
	return start
}

// connectRooms creates entry points from room boundaries to adjacent maze passages
func connectRooms(grid [][]bool, rooms []resolvedRoom, rng *rand.Rand) []RoomResult {
	rows, cols := len(grid), len(grid[0])
	results := make([]RoomResult, 0, len(rooms))

	for _, room := range rooms {
		result := RoomResult{
			X:       room.x,
			Y:       room.y,
			Width:   room.w,
			Height:  room.h,
			Entries: make([]Point, 0, 4),
		}

		// Collect candidate entry points (walls adjacent to room that border passages)
		type candidate struct {
			wallX, wallY   int // Wall cell to remove
			checkX, checkY int // Adjacent cell to verify is passage
		}
		var candidates []candidate

		// Top edge
		for x := room.x; x < room.x+room.w; x++ {
			wy := room.y - 1
			cy := room.y - 2
			if wy > 0 && cy >= 0 && grid[wy][x] == Wall && grid[cy][x] == Passage {
				candidates = append(candidates, candidate{x, wy, x, cy})
			}
		}

		// Bottom edge
		for x := room.x; x < room.x+room.w; x++ {
			wy := room.y + room.h
			cy := room.y + room.h + 1
			if wy < rows && cy < rows && grid[wy][x] == Wall && grid[cy][x] == Passage {
				candidates = append(candidates, candidate{x, wy, x, cy})
			}
		}

		// Left edge
		for y := room.y; y < room.y+room.h; y++ {
			wx := room.x - 1
			cx := room.x - 2
			if wx > 0 && cx >= 0 && grid[y][wx] == Wall && grid[y][cx] == Passage {
				candidates = append(candidates, candidate{wx, y, cx, y})
			}
		}

		// Right edge
		for y := room.y; y < room.y+room.h; y++ {
			wx := room.x + room.w
			cx := room.x + room.w + 1
			if wx < cols && cx < cols && grid[y][wx] == Wall && grid[y][cx] == Passage {
				candidates = append(candidates, candidate{wx, y, cx, y})
			}
		}

		// Create entries (at least 1, up to 4)
		if len(candidates) > 0 {
			// Shuffle candidates
			rng.Shuffle(len(candidates), func(i, j int) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			})

			// Determine entry count: 1-4, biased toward fewer
			maxEntries := len(candidates)
			if maxEntries > 4 {
				maxEntries = 4
			}
			entryCount := 1 + rng.Intn(maxEntries)

			for i := 0; i < entryCount && i < len(candidates); i++ {
				c := candidates[i]
				grid[c.wallY][c.wallX] = Passage
				result.Entries = append(result.Entries, Point{X: c.wallX, Y: c.wallY})
			}
		}

		// Fallback: force at least one entry by carving toward nearest passage
		if len(result.Entries) == 0 {
			entry := forceRoomEntry(grid, room)
			if entry.X >= 0 {
				result.Entries = append(result.Entries, entry)
			}
		}

		results = append(results, result)
	}

	return results
}

// forceRoomEntry carves a path from room edge to nearest passage
func forceRoomEntry(grid [][]bool, room resolvedRoom) Point {
	rows, cols := len(grid), len(grid[0])

	type dir struct {
		dx, dy         int
		startX, startY int
	}

	dirs := []dir{
		{0, -1, room.x + room.w/2, room.y - 1},     // Up
		{0, 1, room.x + room.w/2, room.y + room.h}, // Down
		{-1, 0, room.x - 1, room.y + room.h/2},     // Left
		{1, 0, room.x + room.w, room.y + room.h/2}, // Right
	}

	for _, d := range dirs {
		x, y := d.startX, d.startY

		for steps := 0; steps < 20; steps++ {
			if x < 0 || x >= cols || y < 0 || y >= rows {
				break
			}

			if grid[y][x] == Passage {
				// Found passage; carve back to room
				for cx, cy := x-d.dx, y-d.dy; ; cx, cy = cx-d.dx, cy-d.dy {
					if cx < 0 || cx >= cols || cy < 0 || cy >= rows {
						break
					}
					if cx >= room.x && cx < room.x+room.w &&
						cy >= room.y && cy < room.y+room.h {
						// Reached room interior
						return Point{X: cx + d.dx, Y: cy + d.dy}
					}
					grid[cy][cx] = Passage
				}
				break
			}

			grid[y][x] = Passage
			x += d.dx
			y += d.dy
		}
	}

	return Point{X: -1, Y: -1}
}

// --- Core Algorithms ---

func recursiveBacktracker(grid [][]bool, start Point, rng *rand.Rand) {
	rows, cols := len(grid), len(grid[0])

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

	for y := 1; y < rows-1; y += 2 {
		for x := 1; x < cols-1; x += 2 {
			if grid[y][x] == Wall {
				continue
			}

			exits := 0
			checkDirs := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
			for _, d := range checkDirs {
				if grid[y+d.Y][x+d.X] == Passage {
					exits++
				}
			}

			if exits == 1 && rng.Float64() < probability {
				candidates := make([]Point, 0, 4)

				jumpDirs := []Point{{0, -2}, {0, 2}, {-2, 0}, {2, 0}}
				for _, jd := range jumpDirs {
					nx, ny := x+jd.X, y+jd.Y
					wx, wy := x+jd.X/2, y+jd.Y/2

					if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
						if grid[ny][nx] == Passage && grid[wy][wx] == Wall {
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
// 1. Plazas (2x2 Passages)
// 2. Pillars (Isolated Walls)
func canSafelyRemoveWall(grid [][]bool, x, y int) bool {
	rows, cols := len(grid), len(grid[0])

	isP := func(tx, ty int) bool {
		if tx < 0 || tx >= cols || ty < 0 || ty >= rows {
			return false
		}
		return grid[ty][tx] == Passage
	}

	// Check 1: No Plazas (2x2 Open Space)
	if isP(x-1, y-1) && isP(x, y-1) && isP(x-1, y) {
		return false
	}
	if isP(x, y-1) && isP(x+1, y-1) && isP(x+1, y) {
		return false
	}
	if isP(x-1, y) && isP(x-1, y+1) && isP(x, y+1) {
		return false
	}
	if isP(x+1, y) && isP(x, y+1) && isP(x+1, y+1) {
		return false
	}

	// Check 2: No Pillars (Isolated Walls)
	ortho := []Point{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	for _, d := range ortho {
		nx, ny := x+d.X, y+d.Y
		if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
			if grid[ny][nx] == Wall {
				wallConnections := 0
				for _, d2 := range ortho {
					nnx, nny := nx+d2.X, ny+d2.Y
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
					return false
				}
			}
		}
	}

	return true
}

func stripBorders(grid [][]bool) {
	rows, cols := len(grid), len(grid[0])
	for x := 0; x < cols; x++ {
		grid[0][x] = Passage
		grid[rows-1][x] = Passage
	}
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
		return n - 1
	}
	return n
}

func resolvePoint(h, w int, p *Point, defX, defY int) Point {
	if p == nil {
		return Point{defX, defY}
	}
	x, y := p.X, p.Y
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