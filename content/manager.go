package content

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	assetsDir = "./assets"
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
