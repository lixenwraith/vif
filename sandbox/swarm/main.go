package main

import (
	"fmt"
	"strings"
	"time"
)

// Dimensions of the entity
const (
	Width  = 4
	Height = 2
)

// A Frame is a 2x4 grid of runes
type Frame [Height][Width]rune

// Animation holds the sequence of frames; its id is its place in the catalogue, from 1
type Animation struct {
	Frames []Frame
}

func main() {
	// Define the catalogue of visual concepts
	animations := getAnimations()

	// Animation loop parameters
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	frameIdx := 0

	// Clear screen command (ANSI)
	fmt.Print("\033[2J")

	for range ticker.C {
		// Move cursor to top-left
		fmt.Print("\033[H")

		fmt.Println("SWARM SANDBOX (4x2)")
		fmt.Println("--------------------------------------")
		fmt.Println("Press Ctrl+C to exit")
		fmt.Println("")

		// Render animations in a grid (3 columns)
		cols := 3
		for i := 0; i < len(animations); i += cols {
			// Print ids
			for j := 0; j < cols && i+j < len(animations); j++ {
				fmt.Printf("%-29s", fmt.Sprintf("swarm %d", i+j+1))
			}
			fmt.Println()

			// Print Top Row of the Entity
			for j := 0; j < cols && i+j < len(animations); j++ {
				anim := animations[i+j]
				currentFrame := anim.Frames[frameIdx%len(anim.Frames)]
				fmt.Printf("       %s                  ", rowToString(currentFrame[0]))
			}
			fmt.Println()

			// Print Bottom Row of the Entity
			for j := 0; j < cols && i+j < len(animations); j++ {
				anim := animations[i+j]
				currentFrame := anim.Frames[frameIdx%len(anim.Frames)]
				fmt.Printf("       %s                  ", rowToString(currentFrame[1]))
			}
			fmt.Println()
			fmt.Println() // Spacer
		}

		frameIdx++
	}
}

func rowToString(row [Width]rune) string {
	var sb strings.Builder
	for _, r := range row {
		sb.WriteRune(r)
	}
	return sb.String()
}

// getAnimations returns the list of 4x2 concepts
func getAnimations() []Animation {
	return []Animation{
		{
			Frames: []Frame{
				{{'/', '-', '-', '\\'}, {'\\', '_', '_', '/'}}, // Wings Open
				{{'|', '-', '-', '|'}, {'\\', '-', '-', '/'}},  // Wings Folded
			},
		},
		{
			Frames: []Frame{
				{{'┌', 'o', 'o', '┐'}, {'└', '─', '─', '┘'}}, // Eyes Open
				{{'┌', '-', '-', '┐'}, {'└', '~', '~', '┘'}}, // Blink/Chirp
			},
		},
		{
			Frames: []Frame{
				{{'╔', '═', '═', '╗'}, {'╚', '═', '═', '╝'}}, // Bold
				{{'┌', '─', '─', '┐'}, {'└', '─', '─', '┘'}}, // Thin
			},
		},
		{
			Frames: []Frame{
				{{' ', '/', '\\', ' '}, {' ', '\\', '/', ' '}}, // X shape
				{{' ', '|', '|', ' '}, {' ', '|', '|', ' '}},   // Vertical
				{{' ', '\\', '/', ' '}, {' ', '/', '\\', ' '}}, // Inverse X
				{{' ', '-', '-', ' '}, {' ', '-', '-', ' '}},   // Horizontal
			},
		},
		{
			Frames: []Frame{
				{{'[', ' ', ' ', ']'}, {'[', ' ', ' ', ']'}}, // Open
				{{'[', '▓', '▓', ']'}, {'[', '▓', '▓', ']'}}, // Full
			},
		},
		{
			Frames: []Frame{
				{{' ', ' ', ' ', ' '}, {'<', '=', '=', '>'}},   // Flat
				{{'/', ' ', ' ', '\\'}, {'\\', ' ', ' ', '/'}}, // Open Jaw
			},
		},
		{
			Frames: []Frame{
				{{'▄', '▀', '▀', '▄'}, {'▀', '▄', '▄', '▀'}}, // State A
				{{'▀', '▄', '▄', '▀'}, {'▄', '▀', '▀', '▄'}}, // State B (Treads moving)
			},
		},
		{
			Frames: []Frame{
				{{'#', '%', '&', '@'}, {'@', '&', '%', '#'}},
				{{'1', '0', '1', '1'}, {'0', '1', '0', '0'}},
				{{'?', '!', '?', '!'}, {'!', '?', '!', '?'}},
			},
		},
		{
			Frames: []Frame{
				{{' ', '(', 'O', ')'}, {' ', '\\', '_', '/'}}, // Center
				{{'(', 'O', ')', ' '}, {'\\', '_', '/', ' '}}, // Left
				{{' ', '(', 'O', ')'}, {' ', '\\', '_', '/'}}, // Center
				{{' ', ' ', '(', 'O'}, {' ', ' ', '\\', '_'}}, // Right (clipped)
			},
		},
		{
			Frames: []Frame{
				{{'╓', '─', '─', '╖'}, {'╙', '─', '─', '╜'}},
				{{'╒', '═', '═', '╕'}, {'╘', '═', '═', '╛'}},
			},
		},
		{
			Frames: []Frame{
				{{'|', '>', '<', '|'}, {'|', '>', '<', '|'}}, // Intake
				{{'|', '}', '{', '|'}, {'|', '}', '{', '|'}}, // Combustion
			},
		},
		{
			Frames: []Frame{
				{{'/', '^', '^', '\\'}, {' ', 'v', 'v', ' '}}, // Down
				{{'~', '^', '^', '~'}, {' ', ' ', ' ', ' '}},  // Up
			},
		},
		{
			Frames: []Frame{
				{{'░', '░', '░', '░'}, {'▓', '▓', '▓', '▓'}},
				{{'▒', '▒', '▒', '▒'}, {'▒', '▒', '▒', '▒'}},
				{{'▓', '▓', '▓', '▓'}, {'░', '░', '░', '░'}},
			},
		},
		{
			Frames: []Frame{
				{{'(', '/', '/', ')'}, {'(', '/', '/', ')'}},
				{{'(', '-', '-', ')'}, {'(', '-', '-', ')'}},
				{{'(', '\\', '\\', ')'}, {'(', '\\', '\\', ')'}},
			},
		},
		{
			Frames: []Frame{
				{{' ', 'o', 'o', ' '}, {'/', '"', '"', '\\'}},
				{{'\\', 'o', 'o', '/'}, {' ', '"', '"', ' '}},
			},
		},
		{
			Frames: []Frame{
				{{'⊂', '⊃', '⊂', '⊃'}, {'⊂', '⊃', '⊂', '⊃'}},
				{{'⊃', '⊂', '⊃', '⊂'}, {'⊃', '⊂', '⊃', '⊂'}},
			},
		},
		{
			Frames: []Frame{
				{{'[', '•', '•', ']'}, {'[', '•', '•', ']'}},
				{{'[', ' ', ' ', ']'}, {'[', ' ', ' ', ']'}},
			},
		},
		{
			Frames: []Frame{
				{{'>', '>', '>', '>'}, {' ', ' ', ' ', ' '}},
				{{' ', ' ', ' ', ' '}, {'>', '>', '>', '>'}},
			},
		},
		{
			Frames: []Frame{
				{{'▄', ' ', ' ', '▄'}, {' ', '▀', '▀', ' '}},
				{{' ', '▄', '▄', ' '}, {'▀', ' ', ' ', '▀'}},
			},
		},
		{
			Frames: []Frame{
				{{'V', 'V', 'V', 'V'}, {'^', '^', '^', '^'}}, // Closed
				{{'V', ' ', ' ', 'V'}, {'^', ' ', ' ', '^'}}, // Open
			},
		},
	}
}
