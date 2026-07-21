package service

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
)

type ContentService struct {
	manager     *content.ContentManager
	contentPath string

	content    atomic.Pointer[core.PreparedContent]
	consumed   atomic.Int64
	total      atomic.Int64
	generation atomic.Int64

	refreshing atomic.Bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewContentService(path string) *ContentService {
	return &ContentService{
		contentPath: path,
		stopCh:      make(chan struct{}),
	}
}

func (s *ContentService) Name() string           { return "content" }
func (s *ContentService) Dependencies() []string { return nil }

func (s *ContentService) Init() error {
	s.manager = content.NewContentManager()
	if s.contentPath != "" {
		s.manager.SetDataDir(s.contentPath)
	}
	_ = s.manager.DiscoverContentFiles()
	_ = s.manager.PreValidateAllContent()
	s.loadContent()
	return nil
}

func (s *ContentService) Start() error { return nil }

func (s *ContentService) Stop() error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

func (s *ContentService) Contribute(r *engine.Resource) {
	r.Content = &engine.ContentResource{Provider: s}
}

func (s *ContentService) CurrentContent() *core.PreparedContent {
	return s.content.Load()
}

func (s *ContentService) NotifyConsumed(count int) {
	newConsumed := s.consumed.Add(int64(count))
	total := s.total.Load()
	if total == 0 {
		return
	}
	if float64(newConsumed)/float64(total) >= parameter.ContentRefreshThreshold && !s.refreshing.Load() {
		s.triggerRefresh()
	}
}

func (s *ContentService) loadContent() {
	lines, _, err := s.manager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		s.content.Store(&core.PreparedContent{Generation: s.generation.Add(1)})
		s.total.Store(0)
		s.consumed.Store(0)
		return
	}
	blocks := s.groupIntoBlocks(lines)
	s.content.Store(&core.PreparedContent{Blocks: blocks, Generation: s.generation.Add(1)})
	s.total.Store(int64(len(blocks)))
	s.consumed.Store(0)
}

func (s *ContentService) triggerRefresh() {
	if !s.refreshing.CompareAndSwap(false, true) {
		return
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
			return
		}
		blocks := s.groupIntoBlocks(lines)
		s.content.Store(&core.PreparedContent{Blocks: blocks, Generation: s.generation.Add(1)})
		s.total.Store(int64(len(blocks)))
		s.consumed.Store(0)
	})
}

func (s *ContentService) groupIntoBlocks(lines []string) []core.CodeBlock {
	// Grouping logic identical to original content_service.go
	if len(lines) == 0 {
		return []core.CodeBlock{}
	}
	var blocks []core.CodeBlock
	var currentBlock []string
	var currentIndent, braceDepth int

	for _, line := range lines {
		indent := s.getIndentLevel(line)
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		shouldStartNewBlock := len(currentBlock) == 0 ||
			(len(currentBlock) >= parameter.MaxBlockLines) ||
			(braceDepth == 0 && len(currentBlock) >= parameter.MinBlockLines &&
				(indent < currentIndent-parameter.MinIndentChange || indent > currentIndent+parameter.MinIndentChange))

		if shouldStartNewBlock && len(currentBlock) > 0 {
			if len(currentBlock) >= parameter.MinBlockLines {
				blocks = append(blocks, core.CodeBlock{
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
	if len(currentBlock) >= parameter.MinBlockLines {
		blocks = append(blocks, core.CodeBlock{
			Lines:       currentBlock,
			IndentLevel: currentIndent,
			HasBraces:   s.hasBracesInBlock(currentBlock),
		})
	}
	return blocks
}

func (s *ContentService) getIndentLevel(line string) int {
	indent := 0
	for _, ch := range line {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += parameter.TabWidth
		} else {
			break
		}
	}
	return indent
}

func (s *ContentService) hasBracesInBlock(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "{") || strings.Contains(line, "}") {
			return true
		}
	}
	return false
}
