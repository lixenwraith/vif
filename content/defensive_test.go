package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeLine tests the sanitization of ANSI sequences and control characters
func TestSanitizeLine(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal text",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "ANSI color sequence",
			input:    "\x1b[31mRed Text\x1b[0m",
			expected: "Red Text",
		},
		{
			name:     "Tab converted to space",
			input:    "Line\twith\ttabs",
			expected: "Line with tabs",
		},
		{
			name:     "Control characters removed",
			input:    "Line\x00with\x01control\x02chars",
			expected: "Linewithcontrolchars",
		},
		{
			name:     "Multiple ANSI sequences",
			input:    "\x1b[1m\x1b[32mBold Green\x1b[0m Normal",
			expected: "Bold Green Normal",
		},
		{
			name:     "Mixed content",
			input:    "func\x1b[33m main\x1b[0m() {\treturn\x00}",
			expected: "func main() { return}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := cm.sanitizeLine(test.input)
			if result != test.expected {
				t.Errorf("Expected %q, got %q", test.expected, result)
			}
		})
	}
}

// TestHasControlCharacters tests detection of control characters
func TestHasControlCharacters(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Normal text",
			input:    "Hello World",
			expected: false,
		},
		{
			name:     "Tab character (allowed)",
			input:    "Line\twith\ttabs",
			expected: false,
		},
		{
			name:     "Newline character (allowed)",
			input:    "Line\nwith\nnewlines",
			expected: false,
		},
		{
			name:     "ANSI escape sequence",
			input:    "\x1b[31mRed Text\x1b[0m",
			expected: true,
		},
		{
			name:     "Null character",
			input:    "Line\x00with\x00null",
			expected: true,
		},
		{
			name:     "Bell character",
			input:    "Line\x07with\x07bell",
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := cm.hasControlCharacters(test.input)
			if result != test.expected {
				t.Errorf("Expected %v, got %v for input %q", test.expected, result, test.input)
			}
		})
	}
}

// TestIsValidUTF8 tests UTF-8 validation
func TestIsValidUTF8(t *testing.T) {
	cm := NewContentManager()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Valid ASCII",
			input:    "Hello World",
			expected: true,
		},
		{
			name:     "Valid UTF-8 with emoji",
			input:    "Hello 🌍 World",
			expected: true,
		},
		{
			name:     "Valid UTF-8 with Chinese",
			input:    "你好世界",
			expected: true,
		},
		{
			name:     "Invalid UTF-8 sequence",
			input:    string([]byte{0xff, 0xfe, 0xfd}),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := cm.isValidUTF8(test.input)
			if result != test.expected {
				t.Errorf("Expected %v, got %v for input %q", test.expected, result, test.input)
			}
		})
	}
}

// TestValidateFileEncoding tests file encoding validation
func TestValidateFileEncoding(t *testing.T) {
	cm := NewContentManager()
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		content     []byte
		shouldError bool
	}{
		{
			name:        "Valid UTF-8 file",
			content:     []byte("Line 1\nLine 2\nLine 3"),
			shouldError: false,
		},
		{
			name:        "Valid UTF-8 with emoji",
			content:     []byte("Line 1 🎮\nLine 2\nLine 3"),
			shouldError: false,
		},
		{
			name:        "Invalid UTF-8 sequence",
			content:     []byte{0xff, 0xfe, 0xfd, '\n', 'L', 'i', 'n', 'e'},
			shouldError: true,
		},
		{
			name:        "File with ANSI sequences (warning only)",
			content:     []byte("Line 1\n\x1b[31mRed\x1b[0m\nLine 3"),
			shouldError: false, // Should warn but not error
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filePath := filepath.Join(tempDir, test.name+".txt")
			if err := os.WriteFile(filePath, test.content, 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			err := cm.validateFileEncoding(filePath)
			if test.shouldError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !test.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestPreValidateAllContent tests the pre-validation and caching functionality
func TestPreValidateAllContent(t *testing.T) {
	cm := NewContentManager()
	tempDir := t.TempDir()

	// Create test files with varying quality
	testFiles := map[string]string{
		"valid.txt": generateFileContent(MinProcessedLines + 5),
		"invalid_utf8.txt": string([]byte{0xff, 0xfe, 0xfd}),
		"too_few_lines.txt": "Line 1\nLine 2",
		"good.txt": generateFileContent(MinProcessedLines + 10),
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Set content files manually for testing
	cm.contentFiles = []string{
		filepath.Join(tempDir, "valid.txt"),
		filepath.Join(tempDir, "invalid_utf8.txt"),
		filepath.Join(tempDir, "too_few_lines.txt"),
		filepath.Join(tempDir, "good.txt"),
	}

	// Pre-validate all content
	err := cm.PreValidateAllContent()
	if err != nil {
		t.Fatalf("PreValidateAllContent failed: %v", err)
	}

	// Should have cached only valid files (valid.txt and good.txt)
	if len(cm.validatedCache) != 2 {
		t.Errorf("Expected 2 cached files, got %d", len(cm.validatedCache))
	}

	// Verify cached content is valid
	for _, cached := range cm.validatedCache {
		if !cm.ValidateProcessedContent(cached.lines) {
			t.Errorf("Cached content from %s failed validation", cached.filePath)
		}
		t.Logf("Cached file: %s with %d lines", cached.filePath, len(cached.lines))
	}
}

// TestCircuitBreaker tests the circuit breaker functionality
func TestCircuitBreaker(t *testing.T) {
	var cb circuitBreaker

	// Initially closed
	if cb.IsOpen() {
		t.Error("Circuit breaker should start closed")
	}

	// Record failures
	for i := 0; i < CircuitBreakerThreshold-1; i++ {
		cb.recordFailure(nil)
	}

	// Should still be closed
	if cb.IsOpen() {
		t.Error("Circuit breaker should not be open yet")
	}

	// One more failure should open it
	cb.recordFailure(nil)
	if !cb.IsOpen() {
		t.Error("Circuit breaker should be open after threshold failures")
	}

	// Record success should reset
	cb.recordSuccess()
	if cb.IsOpen() {
		t.Error("Circuit breaker should be closed after success")
	}
	if cb.failureCount != 0 {
		t.Errorf("Failure count should be 0 after success, got %d", cb.failureCount)
	}
}

// TestSelectFromValidatedCache tests selecting from the validated cache
func TestSelectFromValidatedCache(t *testing.T) {
	cm := NewContentManager()

	// Empty cache should return error
	_, _, err := cm.selectFromValidatedCache()
	if err == nil {
		t.Error("Expected error for empty cache")
	}

	// Add some validated content
	cm.validatedCache = []validatedContent{
		{
			lines:    generateLines(MinProcessedLines + 5),
			filePath: "test1.txt",
		},
		{
			lines:    generateLines(MinProcessedLines + 10),
			filePath: "test2.txt",
		},
	}

	// Should successfully select from cache
	block, filePath, err := cm.selectFromValidatedCache()
	if err != nil {
		t.Fatalf("selectFromValidatedCache failed: %v", err)
	}

	if len(block) == 0 {
		t.Error("Expected non-empty block")
	}

	if filePath == "" {
		t.Error("Expected non-empty file path")
	}

	// Should be valid
	if !cm.ValidateProcessedContent(block) {
		t.Error("Cache-selected content failed validation")
	}

	t.Logf("Selected from cache: %s with %d lines", filePath, len(block))
}

// TestProcessContentBlockWithSanitization tests that ProcessContentBlock sanitizes content
func TestProcessContentBlockWithSanitization(t *testing.T) {
	cm := NewContentManager()

	input := []string{
		"\x1b[31mRed line\x1b[0m",
		"Normal line",
		"Line\x00with\x01control",
		"// Comment line",
		"",
		"Line\twith\ttabs",
	}

	result := cm.ProcessContentBlock(input)

	// Should have 4 valid lines (Red line, Normal line, Linewithcontrol, Line with tabs)
	// Comments and empty lines are removed
	if len(result) != 4 {
		t.Errorf("Expected 4 processed lines, got %d", len(result))
	}

	// First line should have ANSI codes removed
	if strings.Contains(result[0], "\x1b") {
		t.Error("ANSI sequences should be removed")
	}

	// Third line should have control characters removed
	if strings.Contains(result[2], "\x00") || strings.Contains(result[2], "\x01") {
		t.Error("Control characters should be removed")
	}

	t.Logf("Processed lines: %v", result)
}

// TestSelectRandomBlockWithValidation_CircuitBreaker tests circuit breaker integration
func TestSelectRandomBlockWithValidation_CircuitBreaker(t *testing.T) {
	cm := NewContentManager()

	// Manually open the circuit breaker
	for i := 0; i < CircuitBreakerThreshold; i++ {
		cm.breaker.recordFailure(nil)
	}

	if !cm.breaker.IsOpen() {
		t.Fatal("Circuit breaker should be open")
	}

	// Should immediately return default content
	block, filePath, err := cm.SelectRandomBlockWithValidation()
	if err != nil {
		t.Fatalf("SelectRandomBlockWithValidation failed: %v", err)
	}

	if filePath != "default" {
		t.Errorf("Expected 'default' file path, got %q", filePath)
	}

	// Should be default content
	defaultContent := cm.GetDefaultContent()
	if len(block) != len(defaultContent) {
		t.Errorf("Expected default content length %d, got %d", len(defaultContent), len(block))
	}
}

// TestSelectRandomBlockWithValidation_PreferCache tests that cache is preferred
func TestSelectRandomBlockWithValidation_PreferCache(t *testing.T) {
	cm := NewContentManager()

	// Add validated cache
	cm.validatedCache = []validatedContent{
		{
			lines:    generateLines(MinProcessedLines + 5),
			filePath: "cached.txt",
		},
	}

	// Should use cache even without content files
	block, filePath, err := cm.SelectRandomBlockWithValidation()
	if err != nil {
		t.Fatalf("SelectRandomBlockWithValidation failed: %v", err)
	}

	if filePath != "cached.txt" {
		t.Errorf("Expected 'cached.txt', got %q", filePath)
	}

	if !cm.ValidateProcessedContent(block) {
		t.Error("Cached content failed validation")
	}

	t.Logf("Successfully used cache: %s with %d lines", filePath, len(block))
}

// TestMaxBlockSize tests that block size limits are enforced
func TestMaxBlockSize(t *testing.T) {
	cm := NewContentManager()
	tempDir := t.TempDir()

	// Create a file with more than MaxBlockSize lines
	var lines []string
	for i := 0; i < MaxBlockSize+100; i++ {
		lines = append(lines, "Line content here")
	}
	content := strings.Join(lines, "\n")

	filePath := filepath.Join(tempDir, "huge.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Load and process file
	processedLines, err := cm.loadAndProcessFile(filePath)
	if err != nil {
		t.Fatalf("loadAndProcessFile failed: %v", err)
	}

	// Should not exceed MaxBlockSize
	if len(processedLines) > MaxBlockSize {
		t.Errorf("Processed lines (%d) exceeds MaxBlockSize (%d)", len(processedLines), MaxBlockSize)
	}

	t.Logf("Loaded %d lines from file with %d total lines", len(processedLines), MaxBlockSize+100)
}

// TestPanicRecovery tests that panics are recovered
func TestPanicRecovery(t *testing.T) {
	cm := NewContentManager()

	// Test panic recovery in sanitizeLine (though it shouldn't panic)
	// This is more of a safety check
	result := cm.sanitizeLine("normal text")
	if result != "normal text" {
		t.Errorf("Expected 'normal text', got %q", result)
	}

	// Test ProcessContentBlock doesn't panic on nil or empty slice
	result2 := cm.ProcessContentBlock(nil)
	if len(result2) != 0 {
		t.Errorf("ProcessContentBlock should return empty slice for nil input, got %d items", len(result2))
	}

	// Test with empty slice
	result3 := cm.ProcessContentBlock([]string{})
	if len(result3) != 0 {
		t.Errorf("ProcessContentBlock should return empty slice for empty input, got %d items", len(result3))
	}
}

// TestLoadContentFileWithValidation tests the updated LoadContentFile
func TestLoadContentFileWithValidation(t *testing.T) {
	cm := NewContentManager()
	tempDir := t.TempDir()

	// Valid file
	validFile := filepath.Join(tempDir, "valid.txt")
	validContent := []byte("Line 1\nLine 2\nLine 3")
	if err := os.WriteFile(validFile, validContent, 0644); err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}

	data, err := cm.LoadContentFile(validFile)
	if err != nil {
		t.Errorf("LoadContentFile failed for valid file: %v", err)
	}
	if len(data) == 0 {
		t.Error("Expected non-empty data")
	}

	// Invalid UTF-8 file
	invalidFile := filepath.Join(tempDir, "invalid.txt")
	invalidContent := []byte{0xff, 0xfe, 0xfd}
	if err := os.WriteFile(invalidFile, invalidContent, 0644); err != nil {
		t.Fatalf("Failed to create invalid file: %v", err)
	}

	_, err = cm.LoadContentFile(invalidFile)
	if err == nil {
		t.Error("Expected error for invalid UTF-8 file")
	}

	// Non-existent file
	_, err = cm.LoadContentFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}
