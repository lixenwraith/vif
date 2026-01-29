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

// SwarmPatternActive defines which cells are collision-active, keeping it simple and make the whole box collide
var SwarmPatternActive = [2][2][4]bool{
	// Pattern 0: All Active
	{{true, true, true, true}, {true, true, true, true}},
	// Pattern 1: All Active
	{{true, true, true, true}, {true, true, true, true}},
}