package modes

import (
	"testing"
)



func TestBracketMatchingAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<template>" at y=0
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 't')
	placeChar(ctx, 2, 0, 'e')
	placeChar(ctx, 3, 0, 'm')
	placeChar(ctx, 4, 0, 'p')
	placeChar(ctx, 5, 0, 'l')
	placeChar(ctx, 6, 0, 'a')
	placeChar(ctx, 7, 0, 't')
	placeChar(ctx, 8, 0, 'e')
	placeChar(ctx, 9, 0, '>')

	// Test % from opening angle bracket
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 9 || ctx.CursorY != 0 {
		t.Errorf("%% from '<': expected (9, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing angle bracket
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '>': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}


func TestBracketMatchingNestedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<<a>>" at y=0
	// Positions: <(0) <(1) a(2) >(3) >(4)
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, '<')
	placeChar(ctx, 2, 0, 'a')
	placeChar(ctx, 3, 0, '>')
	placeChar(ctx, 4, 0, '>')

	// Test % from first opening angle bracket - should match last closing
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from outer '<': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from second opening angle bracket - should match first closing
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 3 || ctx.CursorY != 0 {
		t.Errorf("%% from inner '<': expected (3, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from first closing angle bracket - should match second opening
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 1 || ctx.CursorY != 0 {
		t.Errorf("%% from first '>': expected (1, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from last closing angle bracket - should match first opening
	ctx.CursorX = 4
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from outer '>': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}


func TestBracketMatchingMixedTypesWithAngles(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[{<()>}]" at y=0
	// Positions: [(0) {(1) <(2) ((3) )(4) >(5) }(6) ](7)
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, '{')
	placeChar(ctx, 2, 0, '<')
	placeChar(ctx, 3, 0, '(')
	placeChar(ctx, 4, 0, ')')
	placeChar(ctx, 5, 0, '>')
	placeChar(ctx, 6, 0, '}')
	placeChar(ctx, 7, 0, ']')

	// Test % from '[' - should match ']'
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 7 || ctx.CursorY != 0 {
		t.Errorf("%% from '[': expected (7, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '{' - should match '}'
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 6 || ctx.CursorY != 0 {
		t.Errorf("%% from '{': expected (6, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '<' - should match '>'
	ctx.CursorX = 2
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from '<': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '(' - should match ')'
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from '(': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}


func TestBracketMatchingUnmatchedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<" at (0,0) with no closing angle bracket
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 3, 0, 'c')

	// Test % from opening angle bracket with no match - should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched '<': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: ">" at (5,0) with no opening angle bracket
	ctx = createTestContext()
	placeChar(ctx, 5, 0, '>')
	placeChar(ctx, 6, 0, 'x')

	ctx.CursorX = 5
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched '>': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}


func TestBracketMatchingAngleBracketsMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Setup: Opening angle bracket at (0,0), closing angle bracket at (0,3)
	// Line 0: <
	// Line 1: template
	// Line 2: code
	// Line 3: >
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 0, 1, 't')
	placeChar(ctx, 1, 1, 'e')
	placeChar(ctx, 2, 1, 'm')
	placeChar(ctx, 3, 1, 'p')
	placeChar(ctx, 0, 2, 'c')
	placeChar(ctx, 1, 2, 'o')
	placeChar(ctx, 2, 2, 'd')
	placeChar(ctx, 3, 2, 'e')
	placeChar(ctx, 0, 3, '>')

	// Test % from opening angle bracket on line 0
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 3 {
		t.Errorf("%% from '<' at line 0: expected (0, 3), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing angle bracket on line 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '>' at line 3: expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}


func TestBracketMatchingMismatchedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "< ]" at y=0 - mismatched types
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 'x')
	placeChar(ctx, 2, 0, ']')

	// Test % from '<' - should not match ']', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '<' with mismatched ']': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: "< }" at y=1 - mismatched types
	placeChar(ctx, 0, 1, '<')
	placeChar(ctx, 1, 1, 'y')
	placeChar(ctx, 2, 1, '}')

	// Test % from '<' - should not match '}', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 1 {
		t.Errorf("%% from '<' with mismatched '}': expected (0, 1), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}
