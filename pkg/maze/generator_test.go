package maze

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"testing"
)

// shapes span the options at three sizes: braiding off and on, rooms explicit
// and random, borders kept and removed, start and end given and defaulted;
// then a start that is its end, clamped starts that carve nothing, and rooms
// that overlap, crowd or outgrow their maze
var shapes = []Config{
	{Width: 41, Height: 21},
	{Width: 41, Height: 21, Braiding: 0.5, RemoveBorders: true, StartPos: &Point{X: 3, Y: 5}, EndPos: &Point{X: 37, Y: 15}},
	{Width: 160, Height: 60, Braiding: 0.3, RoomCount: 4},
	{Width: 161, Height: 61, RemoveBorders: true, RoomCount: 5, Rooms: []RoomSpec{
		{CenterX: 81, CenterY: 31, Width: 21, Height: 11}, {Width: 9, Height: 7}, {CenterX: 20, CenterY: 15},
		{CenterX: 85, CenterY: 33, Width: 9, Height: 5},
	}},
	{Width: 401, Height: 121, Braiding: 0.5, RemoveBorders: true, RoomCount: 9, Rooms: []RoomSpec{
		{CenterX: 201, CenterY: 61, Width: 61, Height: 21}, {CenterX: 201, CenterY: 41, Width: 13, Height: 7},
		{CenterX: 101, CenterY: 31, Width: 13, Height: 7}, {CenterX: 301, CenterY: 91, Width: 13, Height: 7},
		{CenterX: 5, CenterY: 2, Width: 9, Height: 5}, {CenterX: 397, CenterY: 117, Width: 9, Height: 5},
	}},
	{Width: 401, Height: 121, Braiding: 1, RoomCount: 6, DefaultRoomWidth: 17, DefaultRoomHeight: 9,
		StartPos: &Point{X: 11, Y: 7}, EndPos: &Point{X: 200, Y: 60}},
	{Width: 3, Height: 3, Braiding: 1},
	{Width: 21, Height: 11, RemoveBorders: true, RoomCount: 1, StartPos: &Point{X: -4, Y: 500}, EndPos: &Point{X: 500, Y: -4}},
	{Width: 21, Height: 11, Braiding: 0.6, RoomCount: 1, StartPos: &Point{X: -4, Y: 500}, EndPos: &Point{X: 10, Y: 4}},
	{Width: 41, Height: 21, Braiding: 0.2, RemoveBorders: true, RoomCount: 9, Rooms: []RoomSpec{{Width: 37, Height: 15}, {Width: 25, Height: 9}}},
	{Width: 40, Height: 22, Braiding: 0.7, RoomCount: 1, Rooms: []RoomSpec{{CenterX: 9, CenterY: 9}, {CenterX: 29, CenterY: 9}}},
}

// The digest pins every field of each result and where the caller's stream
// stands after it, since the wall system keeps drawing from it: a change to
// either moves every recorded maze
func TestAMazeIsFixedByItsConfigAndSeed(t *testing.T) {
	h := fnv.New64a()
	for i, s := range shapes {
		h.Write(appendResult(nil, Generate(s)))
		for seed := range uint64(8) {
			src := rand.NewPCG(seed, uint64(i))
			s.Rng = rand.New(src)
			b := appendResult(nil, Generate(s))
			b, _ = src.AppendBinary(b)
			h.Write(b)
		}
	}
	if got, want := h.Sum64(), uint64(0xa9676ad364ecaf1f); got != want {
		t.Fatalf("maze digest %#x, want %#x", got, want)
	}
}

// Each op reseeds the stream, so it lays one maze in the wall system's shape
func BenchmarkGenerate(b *testing.B) {
	for _, size := range []Point{{X: 41, Y: 21}, {X: 161, Y: 61}, {X: 401, Y: 121}} {
		for _, rich := range []bool{false, true} {
			cfg := Config{Width: size.X, Height: size.Y, RemoveBorders: true}
			name := fmt.Sprintf("%dx%d/plain", size.X, size.Y)
			if rich {
				cfg.Braiding, cfg.RoomCount, cfg.DefaultRoomWidth, cfg.DefaultRoomHeight = 0.5, 4, 9, 5
				name = fmt.Sprintf("%dx%d/braided-rooms", size.X, size.Y)
			}
			b.Run(name, func(b *testing.B) {
				src := rand.NewPCG(1, 2)
				cfg.Rng = rand.New(src)
				for b.Loop() {
					src.Seed(1, 2)
					Generate(cfg)
				}
			})
		}
	}
}

// appendResult appends every field of r, a nil slice apart from an empty one
func appendResult(b []byte, r Result) []byte {
	ints := func(v ...int) {
		for _, n := range v {
			b = binary.AppendVarint(b, int64(n))
		}
	}
	points := func(ps []Point) {
		if ps == nil {
			ints(-1)
			return
		}
		ints(len(ps))
		for _, p := range ps {
			ints(p.X, p.Y)
		}
	}
	ints(len(r.Grid))
	for _, row := range r.Grid {
		ints(len(row))
		for _, wall := range row {
			if wall {
				b = append(b, 1)
			} else {
				b = append(b, 0)
			}
		}
	}
	ints(r.Start.X, r.Start.Y, r.End.X, r.End.Y)
	points(r.SolutionPath)
	if r.Rooms == nil {
		ints(-1)
	}
	ints(len(r.Rooms))
	for _, room := range r.Rooms {
		ints(room.X, room.Y, room.Width, room.Height)
		points(room.Entries)
	}
	return b
}
