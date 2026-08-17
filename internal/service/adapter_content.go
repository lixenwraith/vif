package service

import (
	"fmt"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// ContentSource selects where the corpus is read from.
// An empty Dir selects the embedded corpus; Pin restricts the walk to one file.
type ContentSource struct {
	Dir      string
	Pin      string
	Explicit bool
}

// ContentService owns the loaded corpus and hands out blocks on demand.
// The corpus is read once during Init; no disk access occurs afterwards.
type ContentService struct {
	src   ContentSource
	label string
	seed  uint64

	corpus *content.Corpus

	mu     sync.Mutex
	cursor *content.Cursor

	statServed *atomic.Int64
	statFile   *status.AtomicString
}

// NewContentService creates the service for a resolved corpus source.
// The seed is the run's root: block order is simulation input, not I/O.
func NewContentService(src ContentSource, seed uint64) *ContentService {
	return &ContentService{src: src, seed: seed}
}

func (s *ContentService) Name() string           { return "content" }
func (s *ContentService) Dependencies() []string { return nil }
func (s *ContentService) Start() error           { return nil }
func (s *ContentService) Stop() error            { return nil }

// Init loads the whole corpus into memory.
// An explicit path that yields nothing is fatal; a discovered one falls back
// to the embedded corpus, which is a build artifact and must never be empty.
func (s *ContentService) Init() error {
	policy := content.DefaultPolicy()

	fsys, label := s.open()
	corpus, err := content.Load(fsys, policy)

	if err != nil || corpus.IsEmpty() {
		if s.src.Explicit {
			if err == nil {
				err = fmt.Errorf("no usable content files")
			}
			return fmt.Errorf("content %s: %w", label, err)
		}
		s.src, label = ContentSource{}, "embedded"
		if corpus, err = content.Load(asset.DefaultContent, policy); err != nil {
			return fmt.Errorf("content embedded: %w", err)
		}
		if corpus.IsEmpty() {
			return fmt.Errorf("content embedded: corpus is empty")
		}
	}

	s.corpus, s.label = corpus, label
	s.cursor = content.NewCursor(corpus, vmath.NewSeededRand(s.seed, "content"))

	if s.src.Pin != "" && !s.cursor.Pin(s.src.Pin) {
		return fmt.Errorf("content %s: %s has no usable blocks", label, s.src.Pin)
	}
	return nil
}

func (s *ContentService) Contribute(r *engine.Resource) {
	r.Content = &engine.ContentResource{Provider: s}
}

// NextBlock returns the next content block; safe for concurrent use
func (s *ContentService) NextBlock() (core.CodeBlock, bool) {
	s.mu.Lock()
	block, ok := s.cursor.Next()
	name := s.cursor.Source()
	s.mu.Unlock()

	if !ok {
		return core.CodeBlock{}, false
	}
	if s.statServed != nil {
		s.statServed.Add(1)
		s.statFile.Store(name)
	}
	return block, true
}

// PublishStatus registers corpus telemetry.
// Called after GameContext creates the registry; the corpus is already final.
func (s *ContentService) PublishStatus(reg *status.Registry) {
	if reg == nil || s.corpus == nil {
		return
	}
	reg.Strings.Get("content.source").Store(s.label)
	reg.Ints.Get("content.files").Store(int64(len(s.corpus.Sources)))
	reg.Ints.Get("content.blocks").Store(int64(s.corpus.BlockCount()))
	reg.Ints.Get("content.lines").Store(int64(s.corpus.LineCount()))
	reg.Ints.Get("content.rejected").Store(int64(len(s.corpus.Rejected)))
	s.statServed = reg.Ints.Get("content.served")
	s.statFile = reg.Strings.Get("content.file")
}

// Corpus returns the loaded corpus; nil before Init
func (s *ContentService) Corpus() *content.Corpus { return s.corpus }

// Label returns the resolved corpus location, or "embedded"
func (s *ContentService) Label() string { return s.label }

// Pin returns the single file the corpus is restricted to, empty when unpinned.
// Part of the journal's corpus identity: Label reports only the directory.
func (s *ContentService) Pin() string { return s.src.Pin }

// open returns the corpus filesystem and a telemetry label
func (s *ContentService) open() (fs.FS, string) {
	if s.src.Dir == "" {
		return asset.DefaultContent, "embedded"
	}
	return os.DirFS(s.src.Dir), s.src.Dir
}
