package visual

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// SwarmPatternChars defines visual patterns for swarm composite
var SwarmPatternChars = [2][2][4]rune{
	// Pattern 0: Pulse State O
	{
		{'▄', '▀', '▀', '▄'},
		{'▀', '▄', '▄', '▀'},
	},
	// Pattern 1: Pulse State X
	{
		{'▀', '▄', '▄', '▀'},
		{'▄', '▀', '▀', '▄'},
	},
}

// // SwarmPatternChars defines visual patterns for swarm composite
// var SwarmPatternChars = [3][2][4]rune{
// 	// Pattern 0: Pulse State A (Bold/Expanded) - "The Aggressor"
// 	{
// 		{'╔', '═', '═', '╗'},
// 		{'╚', '═', '═', '╝'},
// 	},
// 	// Pattern 1: Pulse State B (Thin/Contracted) - "The Drone"
// 	{
// 		{'┌', '─', '─', '┐'},
// 		{'└', '─', '─', '┘'},
// 	},
// 	// Pattern 2: Attack/Transition State (Mix) - "The Glitch"
// 	{
// 		{'╓', '─', '─', '╖'},
// 		{'╙', '─', '─', '╜'},
// 	},
// }