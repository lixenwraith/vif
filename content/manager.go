package content

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

const (
	assetsDir        = "./assets"
	ContentBlockSize = 30 // Default number of lines per content block (20-50 range)
	MinProcessedLines = 10 // Minimum number of valid lines required after processing
	MaxLineLength    = 80 // Maximum line length to match game width
	MaxRetries       = 5  // Maximum number of retries when selecting content blocks
)

var (
	// CommentPrefixes defines the prefixes that identify comment lines
	CommentPrefixes = []string{"//", "#"}
)

// ContentManager handles discovery and loading of content files
type ContentManager struct {
	contentFiles []string
}

// NewContentManager creates a new content manager
func NewContentManager() *ContentManager {
	return &ContentManager{
		contentFiles: []string{},
	}
}

// DiscoverContentFiles scans the assets directory for all .txt files
// and stores their paths. It handles missing directories gracefully
// and skips hidden files (those starting with .)
func (cm *ContentManager) DiscoverContentFiles() error {
	// Check if assets directory exists
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		log.Printf("Assets directory '%s' does not exist, no content files discovered", assetsDir)
		return nil // Not an error, just no files to discover
	}

	// Read directory entries
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return fmt.Errorf("failed to read assets directory: %w", err)
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
			log.Printf("Skipping hidden file: %s", fileName)
			continue
		}

		// Check if file has .txt extension
		if strings.HasSuffix(fileName, ".txt") {
			filePath := filepath.Join(assetsDir, fileName)
			cm.contentFiles = append(cm.contentFiles, filePath)
			log.Printf("Discovered content file: %s", filePath)
		}
	}

	// Log summary
	if len(cm.contentFiles) == 0 {
		log.Printf("No .txt files found in %s", assetsDir)
	} else {
		log.Printf("Discovered %d content file(s)", len(cm.contentFiles))
	}

	return nil
}

// GetContentFiles returns the list of discovered content files
func (cm *ContentManager) GetContentFiles() []string {
	return cm.contentFiles
}

// LoadContentFile loads and returns the content of a specific file
// This is a stub for future implementation
func (cm *ContentManager) LoadContentFile(path string) ([]byte, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", path)
	}

	// TODO: Implement actual file loading logic
	// For now, just return empty content
	log.Printf("LoadContentFile stub called for: %s", path)
	return []byte{}, nil
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
// It removes comments, empty lines, trims whitespace, and truncates lines that are too long
// Returns the processed lines
func (cm *ContentManager) ProcessContentBlock(lines []string) []string {
	var processed []string

	for _, line := range lines {
		// Trim whitespace
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if len(trimmed) == 0 || cm.isCommentLine(line) {
			continue
		}

		// Truncate lines that are too long
		if len(trimmed) > MaxLineLength {
			trimmed = trimmed[:MaxLineLength]
		}

		processed = append(processed, trimmed)
	}

	return processed
}

// ValidateProcessedContent checks if the processed content meets minimum requirements
// Returns true if content is valid, false otherwise
func (cm *ContentManager) ValidateProcessedContent(lines []string) bool {
	// Check if we have enough lines
	if len(lines) < MinProcessedLines {
		log.Printf("Content validation failed: only %d lines (minimum %d required)", len(lines), MinProcessedLines)
		return false
	}

	// All lines should already be within MaxLineLength due to processing
	// But we can verify for safety
	for i, line := range lines {
		if len(line) > MaxLineLength {
			log.Printf("Content validation failed: line %d exceeds max length (%d > %d)", i, len(line), MaxLineLength)
			return false
		}
	}

	return true
}

// GetContentBlock reads a block of lines from a file starting at startLine
// It skips empty lines and comments, and wraps around to the beginning if needed
// Returns the lines and any error encountered
func (cm *ContentManager) GetContentBlock(filePath string, startLine, size int) ([]string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	// First pass: read all valid content lines
	var validLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if cm.isValidContentLine(line) {
			validLines = append(validLines, strings.TrimSpace(line))
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

// SelectRandomBlock selects a random file and a random block from that file
// Returns the selected lines and the file path, or an error
func (cm *ContentManager) SelectRandomBlock() ([]string, string, error) {
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
	for scanner.Scan() {
		line := scanner.Text()
		if cm.isValidContentLine(line) {
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
	block, err := cm.GetContentBlock(selectedFile, randomStartLine, ContentBlockSize)
	if err != nil {
		return nil, "", err
	}

	log.Printf("Selected random block from %s starting at line %d (%d lines)", selectedFile, randomStartLine, len(block))
	return block, selectedFile, nil
}

// SelectRandomBlockWithValidation selects a random block and validates it
// If the block doesn't meet requirements, it retries with different blocks
// Falls back to default content if no valid content can be found
// Returns the validated lines, the file path (or "default" for fallback), and any error
func (cm *ContentManager) SelectRandomBlockWithValidation() ([]string, string, error) {
	// Try to get valid content with retries
	for attempt := 0; attempt < MaxRetries; attempt++ {
		// Try to select a random block
		block, filePath, err := cm.SelectRandomBlock()
		if err != nil {
			// If we have no content files, fall back immediately
			if len(cm.contentFiles) == 0 {
				log.Printf("No content files available, using default content")
				return cm.GetDefaultContent(), "default", nil
			}
			log.Printf("Attempt %d: Error selecting random block: %v", attempt+1, err)
			continue
		}

		// Process the block
		processed := cm.ProcessContentBlock(block)

		// Validate the processed content
		if cm.ValidateProcessedContent(processed) {
			log.Printf("Successfully selected and validated content from %s (%d lines)", filePath, len(processed))
			return processed, filePath, nil
		}

		log.Printf("Attempt %d: Content from %s did not meet requirements (%d lines)", attempt+1, filePath, len(processed))
	}

	// All retries failed, fall back to default content
	log.Printf("All %d attempts failed, falling back to default content", MaxRetries)
	return cm.GetDefaultContent(), "default", nil
}
