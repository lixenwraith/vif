package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/vi-fighter/maze"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n=== STOCHASTIC TOPOLOGICAL MAZE GENERATOR ===")

		w := getInt(reader, "Width [Odd prefered] (default 35): ", 35)
		h := getInt(reader, "Height [Odd prefered] (default 19): ", 19)
		braid := getFloat(reader, "Braiding Factor [0.0 - 1.0] (default 0.2): ", 0.2)

		fmt.Print("Mode: Jailbreak (Remove Borders)? [y/N]: ")
		jailStr, _ := reader.ReadString('\n')
		jailMode := strings.ToLower(strings.TrimSpace(jailStr)) == "y"

		cfg := maze.Config{
			Width:         w,
			Height:        h,
			Braiding:      braid,
			RemoveBorders: jailMode,
		}

		fmt.Println("\nGenerating...")
		startT := time.Now()
		res := maze.Generate(cfg)
		dur := time.Since(startT)

		fmt.Printf("Done in %v\n", dur)
		fmt.Printf("Grid Dimensions: %dx%d\n", len(res.Grid[0]), len(res.Grid))

		if res.SolutionPath != nil {
			fmt.Printf("Solution Path Length: %d steps\n", len(res.SolutionPath))
		} else {
			fmt.Println("Status: Unsolvable (Isolated Start/End)")
		}

		draw(res)

		fmt.Print("\nGenerate another? [Y/n]: ")
		cont, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(cont)) == "n" {
			break
		}
	}
}

func draw(res maze.Result) {
	pathMap := make(map[maze.Point]bool)
	for _, p := range res.SolutionPath {
		pathMap[p] = true
	}

	// Double buffering for string builder can be faster, but direct print is fine for terminal
	for y, row := range res.Grid {
		for x, isWall := range row {
			p := maze.Point{X: x, Y: y}

			if p == res.Start {
				fmt.Print("S")
			} else if p == res.End {
				fmt.Print("E")
			} else if isWall {
				fmt.Print("█")
			} else if pathMap[p] {
				// Use a distinct but lighter char for path
				fmt.Print("•")
			} else {
				fmt.Print(" ")
			}
		}
		fmt.Println()
	}
}

// --- Input Helpers ---

func getInt(r *bufio.Reader, prompt string, def int) int {
	fmt.Print(prompt)
	s, _ := r.ReadString('\n')
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func getFloat(r *bufio.Reader, prompt string, def float64) float64 {
	fmt.Print(prompt)
	s, _ := r.ReadString('\n')
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	// Clamp
	if v < 0.0 {
		return 0.0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}