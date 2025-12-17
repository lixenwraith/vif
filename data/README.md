# Vi-Fighter Data Content Files

This directory contains content files used for both testing and actual gameplay in Vi-Fighter. These files provide diverse text content that players navigate through using Vi-like commands.

## Content File Format

All content files should follow these guidelines:

### File Naming Convention

- `code_<language>.txt` - Programming language samples (e.g., `code_go.txt`, `code_python.txt`, `code_rust.txt`)
- `prose_<type>.txt` - Prose content of various types (e.g., `prose_technical.txt`)
- `test_<purpose>.txt` - Specialized test files for specific edge cases

### Content Requirements

Each content file should include:

1. **Minimum Length**: At least 200 lines to provide adequate gameplay variety
2. **Comment Lines**: Include language-appropriate comments for testing comment handling
   - Go/Rust: `//` single-line and `/* */` multi-line
   - Python: `#` single-line and `"""` docstrings
3. **Empty Lines**: Include blank lines to test line processing and navigation
4. **Varied Line Lengths**: Mix of short and long lines to test wrapping and navigation
5. **Complexity Variety**: Range from simple to complex structures

### Content Categories

#### Code Files (`code_*.txt`)

Programming language samples should include:

- Standard library implementations
- Various language constructs (functions, structs, classes, interfaces)
- Documentation comments and inline comments
- Import/use statements
- Type definitions and implementations
- Error handling patterns
- Common idioms for the language

**Recommended Sources**:
- Language standard libraries (public domain or permissive licenses)
- Official language documentation examples
- Your own original code samples

#### Prose Files (`prose_*.txt`)

Technical documentation and prose content should include:

- `prose_technical.txt` - Technical documentation, architecture descriptions, protocol specifications
- Well-structured paragraphs with varied lengths
- Section headers and logical organization
- Mix of short and long sentences

**Recommended Sources**:
- RFC documents (public domain)
- Technical specifications
- Your own technical writing
- Public domain technical documentation

#### Test Files (`test_*.txt`)

Specialized files for testing specific behaviors:

- `test_edge_cases.txt` - Edge cases like very long lines, special characters
- `test_mostly_comments.txt` - High ratio of comment lines
- `test_long_lines.txt` - Lines exceeding typical terminal widths
- `.hidden.txt` - Hidden files (files starting with `.`)

## Creating New Content Files

When adding new content files:

1. **Check Licensing**: Ensure content is permissible to include (public domain, permissive license, or your own work)
2. **Follow Format**: Use appropriate syntax for the content type
3. **Test Variety**: Include diverse patterns to provide good gameplay experience
4. **Document Source**: Add a comment at the top noting the source/origin if applicable
5. **Validate Length**: Ensure file meets minimum 200-line requirement

### Example Header Format

```
// Source: Go Standard Library - encoding/json
// License: BSD 3-Clause (Go Authors)
// Modified for vi-fighter gameplay

package json
...
```

## File Usage

These files are used by:

1. **ContentManager**: Loads and provides content to the spawn system
2. **SpawnSystem**: Spawns enemies with content from these files
3. **Test Suite**: Validates content processing and navigation
4. **Gameplay**: Provides actual content that players navigate through

## Adding New Languages

To add support for a new programming language:

1. Create `code_<language>.txt` in this directory
2. Include representative samples from the language'r standard library
3. Ensure proper comment syntax is included
4. Add variety in code structure and complexity
5. Meet the 200+ line minimum requirement

## Maintenance

When updating content files:

- Preserve existing line count to avoid breaking save game compatibility
- Test with the game to ensure content displays correctly
- Verify comment detection works properly
- Check that special characters render correctly