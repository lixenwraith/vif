package content

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/constant"
)

var (
	// CommentPrefixes defines the prefixes that identify comment lines
	CommentPrefixes = []string{"//", "#"}
)

// findProjectRoot finds the project root by looking for go.mod
// Returns the directory containing go.mod, or current directory if not found
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}

	// Walk up the directory tree looking for go.mod
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		// If we've reached the root, stop
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// circuitBreaker tracks failures and prevents excessive retries
type circuitBreaker struct {
	mu               sync.RWMutex
	failureCount     int
	isOpen           bool
	lastFailureError error
}

func (cb *circuitBreaker) recordFailure(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureError = err

	if cb.failureCount >= constant.CircuitBreakerThreshold {
		cb.isOpen = true
	}
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	cb.isOpen = false
}

func (cb *circuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.isOpen
}

// validatedContent represents pre-validated content cache
type validatedContent struct {
	lines    []string
	filePath string
}

// ContentManager handles discovery and loading of content files
type ContentManager struct {
	contentFiles   []string
	breaker        circuitBreaker
	validatedCache []validatedContent
	cacheMu        sync.RWMutex
	dataDir        string
}

// NewContentManager creates a new content manager
// It automatically finds the project root and uses data/ directory from there
func NewContentManager() *ContentManager {
	projectRoot := findProjectRoot()
	dataPath := filepath.Join(projectRoot, "data")

	return &ContentManager{
		contentFiles:   []string{},
		validatedCache: []validatedContent{},
		dataDir:        dataPath,
	}
}

// safeOperation wraps an operation with panic recovery
// Returns default content on panic
func (cm *ContentManager) safeOperation(operation func() ([]string, error), operationName string) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			lines = cm.GetDefaultContent()
			err = fmt.Errorf("panic in %s: %v", operationName, r)
			cm.breaker.recordFailure(err)
		}
	}()

	lines, err = operation()
	if err != nil {
		cm.breaker.recordFailure(err)
	}
	return lines, err
}

// isValidUTF8 checks if a string is valid UTF-8
func (cm *ContentManager) isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}

// hasControlCharacters checks if a string contains terminal control sequences or harmful control characters
// Allows tabs and newlines, but rejects other control characters
func (cm *ContentManager) hasControlCharacters(s string) bool {
	for _, r := range s {
		// Allow tab and newline
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		// Reject other control characters (0x00-0x1F except tab/newline, and 0x7F-0x9F)
		if unicode.IsControl(r) {
			return true
		}
		// Check for ANSI escape sequences (ESC character)
		if r == '\x1b' {
			return true
		}
	}
	return false
}

// sanitizeLine removes ANSI sequences and control characters from a line
// Returns the sanitized line
func (cm *ContentManager) sanitizeLine(line string) string {
	var result strings.Builder
	result.Grow(len(line))

	inEscapeSeq := false
	for _, r := range line {
		// Detect ANSI escape sequence start
		if r == '\x1b' {
			inEscapeSeq = true
			continue
		}

		// Skip characters in escape sequence until we find the terminator
		if inEscapeSeq {
			// ANSI escape sequences typically end with a letter
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscapeSeq = false
			}
			continue
		}

		// Skip control characters (except tab which we'll convert to space)
		if unicode.IsControl(r) {
			if r == '\t' {
				result.WriteRune(' ')
			}
			continue
		}

		// Keep printable characters
		if unicode.IsPrint(r) || r == ' ' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// validateFileEncoding checks if a file is valid UTF-8 and doesn't contain harmful control sequences
func (cm *ContentManager) validateFileEncoding(filePath string) error {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery
		}
	}()

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Check UTF-8 validity
		if !cm.isValidUTF8(line) {
			return fmt.Errorf("invalid UTF-8 encoding at line %d", lineNum)
		}

		// Check for control characters (but don't fail, we'll sanitize instead)
		// Silent - control characters will be sanitized
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	return nil
}

// DiscoverContentFiles scans the data directory for all .txt files
// and stores their paths. It handles missing directories gracefully
// and skips hidden files (those starting with .)
func (cm *ContentManager) DiscoverContentFiles() error {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery - continuing with no files
		}
	}()

	// Check if data directory exists
	if _, err := os.Stat(cm.dataDir); os.IsNotExist(err) {
		return nil // Not an error, just no files to discover
	}

	// Read directory entries
	entries, err := os.ReadDir(cm.dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	// Clear existing content files
	cm.contentFiles = []string{}

	// Scan for .txt files
	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()

		// Skip hidden files (starting with .)
		if strings.HasPrefix(fileName, ".") {
			continue
		}

		// Check if file has .txt extension
		if strings.HasSuffix(fileName, ".txt") {
			filePath := filepath.Join(cm.dataDir, fileName)
			cm.contentFiles = append(cm.contentFiles, filePath)
		}
	}

	return nil
}

// PreValidateAllContent validates all discovered content files and builds a cache
// This should be called after DiscoverContentFiles during initialization
func (cm *ContentManager) PreValidateAllContent() error {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery - cache may be incomplete
		}
	}()

	cm.cacheMu.Lock()
	defer cm.cacheMu.Unlock()

	cm.validatedCache = []validatedContent{}

	if len(cm.contentFiles) == 0 {
		return nil
	}

	for _, filePath := range cm.contentFiles {
		// Validate file encoding
		if err := cm.validateFileEncoding(filePath); err != nil {
			continue
		}

		// Try to load and process the entire file
		lines, err := cm.loadAndProcessFile(filePath)
		if err != nil {
			continue
		}

		// Validate the processed content
		if !cm.ValidateProcessedContent(lines) {
			continue
		}

		// Set to validated cache
		cm.validatedCache = append(cm.validatedCache, validatedContent{
			lines:    lines,
			filePath: filePath,
		})
	}

	return nil
}

// loadAndProcessFile loads an entire file and processes all lines
func (cm *ContentManager) loadAndProcessFile(filePath string) ([]string, error) {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery
		}
	}()

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++

		// Check for maximum block size to prevent memory issues
		if lineCount > constant.MaxBlockSize {
			break
		}

		line := scanner.Text()

		// Sanitize the line
		sanitized := cm.sanitizeLine(line)

		// Skip if line becomes empty after sanitization
		trimmed := strings.TrimSpace(sanitized)
		if len(trimmed) == 0 || cm.isCommentLine(trimmed) {
			continue
		}

		// Truncate if too long
		if len(trimmed) > constant.MaxLineLength {
			trimmed = trimmed[:constant.MaxLineLength]
		}

		allLines = append(allLines, trimmed)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return allLines, nil
}

// GetContentFiles returns the list of discovered content files
func (cm *ContentManager) GetContentFiles() []string {
	return cm.contentFiles
}

// LoadContentFile loads and returns the content of a specific file
// Includes panic recovery and validation
func (cm *ContentManager) LoadContentFile(path string) ([]byte, error) {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery
		}
	}()

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", path)
	}

	// Validate file encoding first
	if err := cm.validateFileEncoding(path); err != nil {
		return nil, err
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return data, nil
}

// isCommentLine checks if a line starts with any comment prefix
func (cm *ContentManager) isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range CommentPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// isValidContentLine checks if a line is valid content (non-empty, non-comment)
func (cm *ContentManager) isValidContentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) > 0 && !cm.isCommentLine(line)
}

// ProcessContentBlock cleans and prepares a block of text lines for use in game
// It removes comments, empty lines, trims whitespace, sanitizes control characters,
// and truncates lines that are too long. Returns the processed lines
func (cm *ContentManager) ProcessContentBlock(lines []string) []string {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery - returning empty slice
		}
	}()

	var processed []string

	for _, line := range lines {
		// Sanitize the line first (removes ANSI sequences and control characters)
		sanitized := cm.sanitizeLine(line)

		// Trim whitespace
		trimmed := strings.TrimSpace(sanitized)

		// Skip empty lines and comments
		if len(trimmed) == 0 || cm.isCommentLine(trimmed) {
			continue
		}

		// Truncate lines that are too long
		if len(trimmed) > constant.MaxLineLength {
			trimmed = trimmed[:constant.MaxLineLength]
		}

		processed = append(processed, trimmed)
	}

	return processed
}

// ValidateProcessedContent checks if the processed content meets minimum requirements
// Returns true if content is valid, false otherwise
func (cm *ContentManager) ValidateProcessedContent(lines []string) bool {
	// Check if we have enough lines
	if len(lines) < constant.MinProcessedLines {
		return false
	}

	// All lines should already be within MaxLineLength due to processing
	// But we can verify for safety
	for _, line := range lines {
		if len(line) > constant.MaxLineLength {
			return false
		}
	}

	return true
}

// GetContentBlock reads a block of lines from a file starting at startLine
// It skips empty lines and comments, and wraps around to the beginning if needed
// Returns the lines and any error encountered
func (cm *ContentManager) GetContentBlock(filePath string, startLine, size int) ([]string, error) {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery
		}
	}()

	// Enforce maximum size limit
	if size > constant.MaxBlockSize {
		size = constant.MaxBlockSize
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	// First pass: read all valid content lines
	var validLines []string
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++

		// Prevent excessive memory usage
		if lineCount > constant.MaxBlockSize {
			break
		}

		line := scanner.Text()

		// Sanitize the line
		sanitized := cm.sanitizeLine(line)

		if cm.isValidContentLine(sanitized) {
			validLines = append(validLines, strings.TrimSpace(sanitized))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file %s: %w", filePath, err)
	}

	// Check if we have any valid lines
	if len(validLines) == 0 {
		return []string{}, nil
	}

	// Normalize startLine to be within bounds
	startLine = startLine % len(validLines)
	if startLine < 0 {
		startLine = 0
	}

	// Extract the block with wrapping
	var block []string
	for i := 0; i < size && i < len(validLines); i++ {
		lineIndex := (startLine + i) % len(validLines)
		block = append(block, validLines[lineIndex])
	}

	return block, nil
}

// GetDefaultContent returns a default content block to use as fallback
// when no valid content can be loaded from files
func (cm *ContentManager) GetDefaultContent() []string {
	return []string{
		"Welcome to Vi-Fighter!",
		"Type to defeat the falling text",
		"Use Vi motions for combo attacks",
		"Press ESC for normal mode",
		"Press i for insert mode",
		"Press / to search",
		"Delete with d + motion",
		"Jump with f + character",
		"Boost with consecutive moves",
		"Master the combos!",
		"Keep typing to survive",
		"Speed increases over time",
		"Watch your heat meter",
		"Chain commands for points",
		"Good luck, warrior!",
	}
}

// selectFromValidatedCache selects a random block from the pre-validated cache
// Returns the selected lines and the file path, or an error if cache is empty
func (cm *ContentManager) selectFromValidatedCache() ([]string, string, error) {
	cm.cacheMu.RLock()
	defer cm.cacheMu.RUnlock()

	if len(cm.validatedCache) == 0 {
		return nil, "", fmt.Errorf("validated cache is empty")
	}

	// Select a random cached file
	randomIndex := rand.Intn(len(cm.validatedCache))
	cached := cm.validatedCache[randomIndex]

	// Select a random block from the cached lines
	if len(cached.lines) == 0 {
		return nil, "", fmt.Errorf("cached file %s has no valid lines", cached.filePath)
	}

	// Determine block size (use minimum of ContentBlockSize and available lines)
	blockSize := constant.ContentBlockSize
	if len(cached.lines) < blockSize {
		blockSize = len(cached.lines)
	}

	// Select a random starting position
	var startLine int
	if len(cached.lines) > blockSize {
		startLine = rand.Intn(len(cached.lines) - blockSize + 1)
	}

	// Extract the block
	block := make([]string, blockSize)
	for i := 0; i < blockSize; i++ {
		block[i] = cached.lines[(startLine+i)%len(cached.lines)]
	}

	return block, cached.filePath, nil
}

// SelectRandomBlock selects a random file and a random block from that file
// Returns the selected lines and the file path, or an error
func (cm *ContentManager) SelectRandomBlock() ([]string, string, error) {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery
		}
	}()

	// Check if we have any discovered files
	if len(cm.contentFiles) == 0 {
		return nil, "", fmt.Errorf("no content files discovered")
	}

	// Select a random file
	randomFileIndex := rand.Intn(len(cm.contentFiles))
	selectedFile := cm.contentFiles[randomFileIndex]

	// Open the file to count valid lines
	file, err := os.Open(selectedFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file %s: %w", selectedFile, err)
	}
	defer file.Close()

	// Count valid content lines
	var validLineCount int
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++

		// Prevent excessive scanning
		if lineCount > constant.MaxBlockSize {
			break
		}

		line := scanner.Text()
		sanitized := cm.sanitizeLine(line)

		if cm.isValidContentLine(sanitized) {
			validLineCount++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading file %s: %w", selectedFile, err)
	}

	// Check if we have any valid lines
	if validLineCount == 0 {
		return []string{}, selectedFile, nil
	}

	// Select a random starting line
	randomStartLine := rand.Intn(validLineCount)

	// Get the content block
	block, err := cm.GetContentBlock(selectedFile, randomStartLine, constant.ContentBlockSize)
	if err != nil {
		return nil, "", err
	}

	return block, selectedFile, nil
}

// SelectRandomBlockWithValidation selects a random block and validates it
// If the block doesn't meet requirements, it retries with different blocks
// Falls back to default content if no valid content can be found
// Uses circuit breaker to prevent excessive retries after multiple failures
// Prefers validated cache when available
// Returns the validated lines, the file path (or "default" for fallback), and any error
func (cm *ContentManager) SelectRandomBlockWithValidation() ([]string, string, error) {
	// Wrap in panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Silent recovery - returning default content
		}
	}()

	// Check if circuit breaker is open
	if cm.breaker.IsOpen() {
		return cm.GetDefaultContent(), "default", nil
	}

	// Try to use validated cache first (most reliable)
	if len(cm.validatedCache) > 0 {
		block, filePath, err := cm.selectFromValidatedCache()
		if err == nil && cm.ValidateProcessedContent(block) {
			cm.breaker.recordSuccess()
			return block, filePath, nil
		}
	}

	// Try to get valid content with retries
	for attempt := 0; attempt < constant.MaxRetries; attempt++ {
		// Try to select a random block
		block, filePath, err := cm.SelectRandomBlock()
		if err != nil {
			// If we have no content files, fall back immediately
			if len(cm.contentFiles) == 0 {
				cm.breaker.recordFailure(err)
				return cm.GetDefaultContent(), "default", nil
			}
			continue
		}

		// Process the block
		processed := cm.ProcessContentBlock(block)

		// Validate the processed content
		if cm.ValidateProcessedContent(processed) {
			cm.breaker.recordSuccess()
			return processed, filePath, nil
		}
	}

	// All retries failed, fall back to default content
	err := fmt.Errorf("all %d attempts failed to load valid content", constant.MaxRetries)
	cm.breaker.recordFailure(err)
	return cm.GetDefaultContent(), "default", nil
}