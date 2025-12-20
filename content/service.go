package content

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/service"
)

// Service manages content loading and provides block access to SpawnSystem
type Service struct {
	manager *ContentManager

	// Atomic pointer to current content batch
	content atomic.Pointer[PreparedContent]

	// Consumption tracking for refresh triggering
	consumed   atomic.Int64
	total      atomic.Int64
	generation atomic.Int64

	// Refresh state
	refreshing atomic.Bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewService creates a new content service
func NewService() *Service {
	return &Service{
		stopCh: make(chan struct{}),
	}
}

// Name implements Service
func (s *Service) Name() string {
	return "content"
}

// Dependencies implements Service
func (s *Service) Dependencies() []string {
	return nil
}

// Init implements Service
// args[0]: string - file glob path (optional, defaults to discovered files)
func (s *Service) Init(args ...any) error {
	s.manager = NewContentManager()

	// Override data directory if path provided
	if len(args) > 0 {
		if path, ok := args[0].(string); ok && path != "" {
			s.manager.dataDir = path
		}
	}

	if err := s.manager.DiscoverContentFiles(); err != nil {
		// Continue gracefully with empty content
	}

	if err := s.manager.PreValidateAllContent(); err != nil {
		// Continue gracefully
	}

	s.loadContent()

	return nil
}

// Start implements Service
func (s *Service) Start() error {
	return nil
}

// Stop implements Service
func (s *Service) Stop() error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// Contribute implements service.ResourceContributor
// Publishes ContentProviderResource for ECS system access
func (s *Service) Contribute(publish service.ResourcePublisher) {
	publish(&engine.ContentResource{Provider: s})
}

// CurrentContent returns the current prepared content batch
// Caller should snapshot this at start of frame for consistency
func (s *Service) CurrentContent() *PreparedContent {
	return s.content.Load()
}

// NotifyConsumed reports block consumption to trigger refresh at threshold
func (s *Service) NotifyConsumed(count int) {
	newConsumed := s.consumed.Add(int64(count))
	total := s.total.Load()

	if total == 0 {
		return
	}

	ratio := float64(newConsumed) / float64(total)
	if ratio >= constant.ContentRefreshThreshold && !s.refreshing.Load() {
		s.triggerRefresh()
	}
}

// loadContent loads and processes content from manager
func (s *Service) loadContent() {
	lines, _, err := s.manager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		s.content.Store(&PreparedContent{
			Blocks:     []CodeBlock{},
			Generation: s.generation.Add(1),
		})
		s.total.Store(0)
		s.consumed.Store(0)
		return
	}

	blocks := s.groupIntoBlocks(lines)

	s.content.Store(&PreparedContent{
		Blocks:     blocks,
		Generation: s.generation.Add(1),
	})
	s.total.Store(int64(len(blocks)))
	s.consumed.Store(0)
}

// triggerRefresh starts background content refresh
func (s *Service) triggerRefresh() {
	if !s.refreshing.CompareAndSwap(false, true) {
		return // Already refreshing
	}

	s.wg.Add(1)
	core.Go(func() {
		defer s.wg.Done()
		defer s.refreshing.Store(false)

		select {
		case <-s.stopCh:
			return
		default:
		}

		lines, _, err := s.manager.SelectRandomBlockWithValidation()
		if err != nil || len(lines) == 0 {
			return // Keep current content on failure
		}

		blocks := s.groupIntoBlocks(lines)

		s.content.Store(&PreparedContent{
			Blocks:     blocks,
			Generation: s.generation.Add(1),
		})
		s.total.Store(int64(len(blocks)))
		s.consumed.Store(0)
	})
}

// groupIntoBlocks groups lines into logical code blocks
func (s *Service) groupIntoBlocks(lines []string) []CodeBlock {
	if len(lines) == 0 {
		return []CodeBlock{}
	}

	var blocks []CodeBlock
	var currentBlock []string
	var currentIndent int
	var braceDepth int

	for _, line := range lines {
		indent := s.getIndentLevel(line)

		braceDepth += strings.Count(line, "{")
		braceDepth -= strings.Count(line, "}")

		shouldStartNewBlock := len(currentBlock) == 0 ||
			(len(currentBlock) >= constant.MaxBlockLines) ||
			(braceDepth == 0 && len(currentBlock) >= constant.MinBlockLines &&
				(indent < currentIndent-constant.MinIndentChange || indent > currentIndent+constant.MinIndentChange))

		if shouldStartNewBlock && len(currentBlock) > 0 {
			if len(currentBlock) >= constant.MinBlockLines {
				blocks = append(blocks, CodeBlock{
					Lines:       currentBlock,
					IndentLevel: currentIndent,
					HasBraces:   s.hasBracesInBlock(currentBlock),
				})
			}
			currentBlock = []string{}
			currentIndent = indent
		}

		currentBlock = append(currentBlock, line)
		if len(currentBlock) == 1 {
			currentIndent = indent
		}
	}

	if len(currentBlock) >= constant.MinBlockLines {
		blocks = append(blocks, CodeBlock{
			Lines:       currentBlock,
			IndentLevel: currentIndent,
			HasBraces:   s.hasBracesInBlock(currentBlock),
		})
	}

	return blocks
}

// getIndentLevel counts leading spaces/tabs
func (s *Service) getIndentLevel(line string) int {
	indent := 0
	for _, ch := range line {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += constant.TabWidth
		} else {
			break
		}
	}
	return indent
}

// hasBracesInBlock checks if a block contains braces
func (s *Service) hasBracesInBlock(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "{") || strings.Contains(line, "}") {
			return true
		}
	}
	return false
}