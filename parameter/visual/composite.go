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