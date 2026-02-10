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

		// CHANGED: 0 values now trigger auto-scaling logic in the generator
		roomCount := getInt(reader, "Room Count (default 0): ", 0)
		roomW := getInt(reader, "Default Room Width (0 = Auto): ", 0)
		roomH := getInt(reader, "Default Room Height (0 = Auto): ", 0)

		var startPos, endPos *maze.Point
		fmt.Print("Specify custom Start/End positions? [y/N]: ")
		customStr, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(customStr)) == "y" {
			startPos = &maze.Point{
				X: getInt(reader, "  Start X: ", 1),
				Y: getInt(reader, "  Start Y: ", 1),
			}
			endPos = &maze.Point{
				X: getInt(reader, "  End X: ", w-2),
				Y: getInt(reader, "  End Y: ", h-2),
			}
		}

		cfg := maze.Config{
			Width:             w,
			Height:            h,
			Braiding:          braid,
			RemoveBorders:     jailMode,
			RoomCount:         roomCount,
			DefaultRoomWidth:  roomW,
			DefaultRoomHeight: roomH,
			StartPos:          startPos,
			EndPos:            endPos,
		}

		// ADDED: Support for partially random RoomSpecs (0 = random for that field)
		if roomCount > 0 {
			fmt.Print("Define explicit room specs? (0 for random field) [y/N]: ")
			exStr, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(exStr)) == "y" {
				numSpecs := getInt(reader, "  How many specs?: ", 1)
				for i := 0; i < numSpecs; i++ {
					fmt.Printf("  --- Room Spec %d ---\n", i+1)
					cfg.Rooms = append(cfg.Rooms, maze.RoomSpec{
						CenterX: getInt(reader, "    Center X (0=rnd): ", 0),
						CenterY: getInt(reader, "    Center Y (0=rnd): ", 0),
						Width:   getInt(reader, "    Width    (0=auto): ", 0),
						Height:  getInt(reader, "    Height   (0=auto): ", 0),
					})
				}
			}
		}

		fmt.Println("\nGenerating...")
		startT := time.Now()
		res := maze.Generate(cfg)
		dur := time.Since(startT)

		fmt.Printf("Done in %v\n", dur)
		fmt.Printf("Rooms placed: %d/%d\n", len(res.Rooms), roomCount)

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

	roomMap := make(map[maze.Point]bool)
	entryMap := make(map[maze.Point]bool)
	for _, r := range res.Rooms {
		for y := r.Y; y < r.Y+r.Height; y++ {
			for x := r.X; x < r.X+r.Width; x++ {
				roomMap[maze.Point{X: x, Y: y}] = true
			}
		}
		for _, e := range r.Entries {
			entryMap[e] = true
		}
	}

	for y, row := range res.Grid {
		for x, isWall := range row {
			p := maze.Point{X: x, Y: y}

			if p == res.Start {
				fmt.Print("S")
			} else if p == res.End {
				fmt.Print("E")
			} else if isWall {
				fmt.Print("█")
			} else if entryMap[p] {
				fmt.Print("+")
			} else if pathMap[p] {
				fmt.Print("•")
			} else if roomMap[p] {
				fmt.Print(".")
			} else {
				fmt.Print(" ")
			}
		}
		fmt.Println()
	}
}

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
	if v < 0.0 {
		return 0.0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}