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

// Animation holds the sequence of frames and a name
type Animation struct {
	Name   string
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

		fmt.Println("VI-FIGHTER SWARM VISUAL CONCEPTS (4x2)")
		fmt.Println("--------------------------------------")
		fmt.Println("Press Ctrl+C to exit")
		fmt.Println("")

		// Render animations in a grid (3 columns)
		cols := 3
		for i := 0; i < len(animations); i += cols {
			// Print Names
			for j := 0; j < cols && i+j < len(animations); j++ {
				fmt.Printf("ID %2d: %-20s  ", i+j+1, animations[i+j].Name)
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
			Name: "The Interceptor",
			Frames: []Frame{
				{{'/', '-', '-', '\\'}, {'\\', '_', '_', '/'}}, // Wings Open
				{{'|', '-', '-', '|'}, {'\\', '-', '-', '/'}},  // Wings Folded
			},
		},
		{
			Name: "The Scarab",
			Frames: []Frame{
				{{'┌', 'o', 'o', '┐'}, {'└', '─', '─', '┘'}}, // Eyes Open
				{{'┌', '-', '-', '┐'}, {'└', '~', '~', '┘'}}, // Blink/Chirp
			},
		},
		{
			Name: "The Pulse Box",
			Frames: []Frame{
				{{'╔', '═', '═', '╗'}, {'╚', '═', '═', '╝'}}, // Bold
				{{'┌', '─', '─', '┐'}, {'└', '─', '─', '┘'}}, // Thin
			},
		},
		{
			Name: "The Rotor",
			Frames: []Frame{
				{{' ', '/', '\\', ' '}, {' ', '\\', '/', ' '}}, // X shape
				{{' ', '|', '|', ' '}, {' ', '|', '|', ' '}},   // Vertical
				{{' ', '\\', '/', ' '}, {' ', '/', '\\', ' '}}, // Inverse X
				{{' ', '-', '-', ' '}, {' ', '-', '-', ' '}},   // Horizontal
			},
		},
		{
			Name: "The Data Eater",
			Frames: []Frame{
				{{'[', ' ', ' ', ']'}, {'[', ' ', ' ', ']'}}, // Open
				{{'[', '▓', '▓', ']'}, {'[', '▓', '▓', ']'}}, // Full
			},
		},
		{
			Name: "The Clamp",
			Frames: []Frame{
				{{' ', ' ', ' ', ' '}, {'<', '=', '=', '>'}},   // Flat
				{{'/', ' ', ' ', '\\'}, {'\\', ' ', ' ', '/'}}, // Open Jaw
			},
		},
		{
			Name: "The Hover-Tank",
			Frames: []Frame{
				{{'▄', '▀', '▀', '▄'}, {'▀', '▄', '▄', '▀'}}, // State A
				{{'▀', '▄', '▄', '▀'}, {'▄', '▀', '▀', '▄'}}, // State B (Treads moving)
			},
		},
		{
			Name: "The Glitch",
			Frames: []Frame{
				{{'#', '%', '&', '@'}, {'@', '&', '%', '#'}},
				{{'1', '0', '1', '1'}, {'0', '1', '0', '0'}},
				{{'?', '!', '?', '!'}, {'!', '?', '!', '?'}},
			},
		},
		{
			Name: "The Seeker Eye",
			Frames: []Frame{
				{{' ', '(', 'O', ')'}, {' ', '\\', '_', '/'}}, // Center
				{{'(', 'O', ')', ' '}, {'\\', '_', '/', ' '}}, // Left
				{{' ', '(', 'O', ')'}, {' ', '\\', '_', '/'}}, // Center
				{{' ', ' ', '(', 'O'}, {' ', ' ', '\\', '_'}}, // Right (clipped)
			},
		},
		{
			Name: "The Construct",
			Frames: []Frame{
				{{'╓', '─', '─', '╖'}, {'╙', '─', '─', '╜'}},
				{{'╒', '═', '═', '╕'}, {'╘', '═', '═', '╛'}},
			},
		},
		{
			Name: "The Engine",
			Frames: []Frame{
				{{'|', '>', '<', '|'}, {'|', '>', '<', '|'}}, // Intake
				{{'|', '}', '{', '|'}, {'|', '}', '{', '|'}}, // Combustion
			},
		},
		{
			Name: "The Bat-Wing",
			Frames: []Frame{
				{{'/', '^', '^', '\\'}, {' ', 'v', 'v', ' '}}, // Down
				{{'~', '^', '^', '~'}, {' ', ' ', ' ', ' '}},  // Up
			},
		},
		{
			Name: "The Phase Shift",
			Frames: []Frame{
				{{'░', '░', '░', '░'}, {'▓', '▓', '▓', '▓'}},
				{{'▒', '▒', '▒', '▒'}, {'▒', '▒', '▒', '▒'}},
				{{'▓', '▓', '▓', '▓'}, {'░', '░', '░', '░'}},
			},
		},
		{
			Name: "The Coil",
			Frames: []Frame{
				{{'(', '/', '/', ')'}, {'(', '/', '/', ')'}},
				{{'(', '-', '-', ')'}, {'(', '-', '-', ')'}},
				{{'(', '\\', '\\', ')'}, {'(', '\\', '\\', ')'}},
			},
		},
		{
			Name: "The Invader",
			Frames: []Frame{
				{{' ', 'o', 'o', ' '}, {'/', '"', '"', '\\'}},
				{{'\\', 'o', 'o', '/'}, {' ', '"', '"', ' '}},
			},
		},
		{
			Name: "The Magnet",
			Frames: []Frame{
				{{'⊂', '⊃', '⊂', '⊃'}, {'⊂', '⊃', '⊂', '⊃'}},
				{{'⊃', '⊂', '⊃', '⊂'}, {'⊃', '⊂', '⊃', '⊂'}},
			},
		},
		{
			Name: "The Shielded Core",
			Frames: []Frame{
				{{'[', '•', '•', ']'}, {'[', '•', '•', ']'}},
				{{'[', ' ', ' ', ']'}, {'[', ' ', ' ', ']'}},
			},
		},
		{
			Name: "The Chevron",
			Frames: []Frame{
				{{'>', '>', '>', '>'}, {' ', ' ', ' ', ' '}},
				{{' ', ' ', ' ', ' '}, {'>', '>', '>', '>'}},
			},
		},
		{
			Name: "The Waveform",
			Frames: []Frame{
				{{'▄', ' ', ' ', '▄'}, {' ', '▀', '▀', ' '}},
				{{' ', '▄', '▄', ' '}, {'▀', ' ', ' ', '▀'}},
			},
		},
		{
			Name: "The Jaws",
			Frames: []Frame{
				{{'V', 'V', 'V', 'V'}, {'^', '^', '^', '^'}}, // Closed
				{{'V', ' ', ' ', 'V'}, {'^', ' ', ' ', '^'}}, // Open
			},
		},
	}
}