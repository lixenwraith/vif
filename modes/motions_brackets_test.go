package modes

import (
	"testing"
)

func TestBracketMatchingParentheses(t *testing.T) {
	ctx := createTestContext()

	// Setup: "(hello)" at y=0
	// Positions: ((0) h(1) e(2) l(3) l(4) o(5) )(6)
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'h')
	placeChar(ctx, 2, 0, 'e')
	placeChar(ctx, 3, 0, 'l')
	placeChar(ctx, 4, 0, 'l')
	placeChar(ctx, 5, 0, 'o')
	placeChar(ctx, 6, 0, ')')

	// Test % from opening parenthesis
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 6 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '(': expected (6, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from closing parenthesis (should go back)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from ')': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingCurlyBraces(t *testing.T) {
	ctx := createTestContext()

	// Setup: "{code}" at y=0
	placeChar(ctx, 0, 0, '{')
	placeChar(ctx, 1, 0, 'c')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 3, 0, 'd')
	placeChar(ctx, 4, 0, 'e')
	placeChar(ctx, 5, 0, '}')

	// Test % from opening brace
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 5 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '{': expected (5, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from closing brace
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '}': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingSquareBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[array]" at y=0
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'r')
	placeChar(ctx, 3, 0, 'r')
	placeChar(ctx, 4, 0, 'a')
	placeChar(ctx, 5, 0, 'y')
	placeChar(ctx, 6, 0, ']')

	// Test % from opening bracket
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 6 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '[': expected (6, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from closing bracket
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from ']': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingNested(t *testing.T) {
	ctx := createTestContext()

	// Setup: "((a))" at y=0
	// Positions: ((0) ((1) a(2) )(3) )(4)
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, '(')
	placeChar(ctx, 2, 0, 'a')
	placeChar(ctx, 3, 0, ')')
	placeChar(ctx, 4, 0, ')')

	// Test % from first opening parenthesis - should match last closing
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 4 || getCursorY(ctx) != 0 {
		t.Errorf("%% from outer '(': expected (4, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from second opening parenthesis - should match first closing
	setCursorPosition(ctx, 1, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 3 || getCursorY(ctx) != 0 {
		t.Errorf("%% from inner '(': expected (3, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from first closing parenthesis - should match second opening
	setCursorPosition(ctx, 3, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 1 || getCursorY(ctx) != 0 {
		t.Errorf("%% from first ')': expected (1, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from last closing parenthesis - should match first opening
	setCursorPosition(ctx, 4, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from outer ')': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Setup: Opening brace at (0,0), closing brace at (0,3)
	// Line 0: {
	// Line 1: code
	// Line 2: here
	// Line 3: }
	placeChar(ctx, 0, 0, '{')
	placeChar(ctx, 0, 1, 'c')
	placeChar(ctx, 1, 1, 'o')
	placeChar(ctx, 2, 1, 'd')
	placeChar(ctx, 3, 1, 'e')
	placeChar(ctx, 0, 2, 'h')
	placeChar(ctx, 1, 2, 'e')
	placeChar(ctx, 2, 2, 'r')
	placeChar(ctx, 3, 2, 'e')
	placeChar(ctx, 0, 3, '}')

	// Test % from opening brace on line 0
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 3 {
		t.Errorf("%% from '{' at line 0: expected (0, 3), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from closing brace on line 3
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '}' at line 3: expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingMixedTypes(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[{()}]" at y=0
	// Positions: [(0) {(1) ((2) )(3) }(4) ](5)
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, '{')
	placeChar(ctx, 2, 0, '(')
	placeChar(ctx, 3, 0, ')')
	placeChar(ctx, 4, 0, '}')
	placeChar(ctx, 5, 0, ']')

	// Test % from '[' - should match ']'
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 5 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '[': expected (5, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from '{' - should match '}'
	setCursorPosition(ctx, 1, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 4 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '{': expected (4, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from '(' - should match ')'
	setCursorPosition(ctx, 2, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 3 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '(': expected (3, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingNonBracket(t *testing.T) {
	ctx := createTestContext()

	// Setup: "hello" at y=0
	placeChar(ctx, 0, 0, 'h')
	placeChar(ctx, 1, 0, 'e')
	placeChar(ctx, 2, 0, 'l')
	placeChar(ctx, 3, 0, 'l')
	placeChar(ctx, 4, 0, 'o')

	// Test % from non-bracket character - should stay in place
	setCursorPosition(ctx, 2, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 2 || getCursorY(ctx) != 0 {
		t.Errorf("%% from non-bracket: expected (2, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingNoMatch(t *testing.T) {
	ctx := createTestContext()

	// Setup: "(" at (0,0) with no closing parenthesis
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 3, 0, 'c')

	// Test % from opening parenthesis with no match - should stay in place
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from unmatched '(': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Setup: ")" at (5,0) with no opening parenthesis
	ctx = createTestContext()
	placeChar(ctx, 5, 0, ')')
	placeChar(ctx, 6, 0, 'x')

	setCursorPosition(ctx, 5, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 5 || getCursorY(ctx) != 0 {
		t.Errorf("%% from unmatched ')': expected (5, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingDeeplyNested(t *testing.T) {
	ctx := createTestContext()

	// Setup: "((((a))))" at y=0
	// Positions: ((0) ((1) ((2) ((3) a(4) )(5) )(6) )(7) )(8)
	text := "((((a))))"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	// Test % from outermost opening bracket
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 8 || getCursorY(ctx) != 0 {
		t.Errorf("%% from outermost '(': expected (8, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from innermost opening bracket
	setCursorPosition(ctx, 3, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 5 || getCursorY(ctx) != 0 {
		t.Errorf("%% from innermost '(': expected (5, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from innermost closing bracket
	setCursorPosition(ctx, 5, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 3 || getCursorY(ctx) != 0 {
		t.Errorf("%% from innermost ')': expected (3, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from outermost closing bracket
	setCursorPosition(ctx, 8, getCursorY(ctx))
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from outermost ')': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingComplexMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Simulate a function definition across multiple lines:
	// Line 0: func test() {
	// Line 1:   if (x > 0) {
	// Line 2:     return x
	// Line 3:   }
	// Line 4: }

	// Line 0: func test() {
	text0 := "func test() {"
	for i, r := range text0 {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Line 1:   if (x > 0) {
	text1 := "  if (x > 0) {"
	for i, r := range text1 {
		if r != ' ' {
			placeChar(ctx, i, 1, r)
		}
	}

	// Line 2:     return x
	text2 := "    return x"
	for i, r := range text2 {
		if r != ' ' {
			placeChar(ctx, i, 2, r)
		}
	}

	// Line 3:   }
	placeChar(ctx, 2, 3, '}')

	// Line 4: }
	placeChar(ctx, 0, 4, '}')

	// Test % from opening brace on line 0 - should match closing brace on line 4
	setCursorPosition(ctx, 12, getCursorY(ctx)) // Position of '{' in "func test() {"
	setCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 4 {
		t.Errorf("%% from outer '{' at line 0: expected (0, 4), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from opening parenthesis on line 1
	setCursorPosition(ctx, 5, getCursorY(ctx)) // Position of '(' in "if (x > 0)"
	setCursorPosition(ctx, getCursorX(ctx), 1)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 11 || getCursorY(ctx) != 1 {
		t.Errorf("%% from '(' at line 1: expected (11, 1), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from opening brace on line 1 - should match closing brace on line 3
	setCursorPosition(ctx, 13, getCursorY(ctx)) // Position of '{' at end of line 1
	setCursorPosition(ctx, getCursorX(ctx), 1)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 2 || getCursorY(ctx) != 3 {
		t.Errorf("%% from inner '{' at line 1: expected (2, 3), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingWithGaps(t *testing.T) {
	ctx := createTestContext()

	// Setup: '(' at (0,0), ')' at (50,5) with large gap
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 50, 5, ')')

	// Test % from opening parenthesis - should find closing across gap
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 50 || getCursorY(ctx) != 5 {
		t.Errorf("%% from '(' with gap: expected (50, 5), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Test % from closing parenthesis - should find opening across gap
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from ')' with gap: expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}

func TestBracketMatchingMismatchedTypes(t *testing.T) {
	ctx := createTestContext()

	// Setup: "( ]" at y=0 - mismatched types
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'x')
	placeChar(ctx, 2, 0, ']')

	// Test % from '(' - should not match ']', should stay in place
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 0 {
		t.Errorf("%% from '(' with mismatched ']': expected (0, 0), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}

	// Setup: "[ }" at y=1 - mismatched types
	placeChar(ctx, 0, 1, '[')
	placeChar(ctx, 1, 1, 'y')
	placeChar(ctx, 2, 1, '}')

	// Test % from '[' - should not match '}', should stay in place
	setCursorPosition(ctx, 0, 1)
	ExecuteMotion(ctx, '%', 1)
	if getCursorX(ctx) != 0 || getCursorY(ctx) != 1 {
		t.Errorf("%% from '[' with mismatched '}': expected (0, 1), got (%d, %d)", getCursorX(ctx), getCursorY(ctx))
	}
}
