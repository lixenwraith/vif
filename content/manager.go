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
