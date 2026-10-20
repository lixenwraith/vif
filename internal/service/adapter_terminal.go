//go:build !vif_headless

package service

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"

	"github.com/lixenwraith/terminal"
)

// TerminalService opens the tty and polls its events. A process that runs several
// Apps on one terminal retains it: each run's hub then starts and stops nothing,
// and Release ends it.
type TerminalService struct {
	term      terminal.Terminal
	colorMode terminal.ColorMode
	eventCh   chan terminal.Event
	stopCh    chan struct{}
	doneCh    chan struct{}
	mu        sync.Mutex
	running   bool
	retained  bool
	finiOnce  sync.Once
}

func NewTerminalService(colorMode terminal.ColorMode) *TerminalService {
	return &TerminalService{
		colorMode: colorMode,
		eventCh:   make(chan terminal.Event, 256),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

func (s *TerminalService) Name() string           { return "terminal" }
func (s *TerminalService) Dependencies() []string { return nil }

// Retain keeps the terminal across runs: Stop leaves it polling and in raw mode,
// its events buffered for the next run, until Release.
func (s *TerminalService) Retain() { s.retained = true }

// Init opens the tty; a run replacing another finds it open.
func (s *TerminalService) Init() error {
	if s.term != nil {
		return nil
	}
	term := terminal.New(s.colorMode)
	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal init: %w", err)
	}
	s.term = term
	return nil
}

func (s *TerminalService) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	go s.pollLoop()
	return nil
}

func (s *TerminalService) pollLoop() {
	defer close(s.doneCh)
	defer func() {
		if r := recover(); r != nil {
			terminal.EmergencyReset(os.Stdout)
			os.Stdout.Sync()
			os.Stderr.Sync()
			fmt.Fprintf(os.Stderr, "\r\n\x1b[31mTERMINAL POLL CRASHED: %v\x1b[0m\r\n", r)
			fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())
			os.Stderr.Sync()
			os.Exit(1)
		}
	}()

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		ev := s.term.PollEvent()
		if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
			return
		}
		select {
		case s.eventCh <- ev:
		case <-s.stopCh:
			return
		}
	}
}

// Stop releases the terminal unless the process retains it across its runs.
func (s *TerminalService) Stop() error {
	if !s.retained {
		s.Release()
	}
	return nil
}

// Release ends polling and restores the tty. Unconditional on the tty: Init may
// have succeeded without Start, and a failed startup must not leave it in raw mode.
func (s *TerminalService) Release() {
	s.mu.Lock()
	wasRunning := s.running
	s.running = false
	s.mu.Unlock()

	if wasRunning {
		close(s.stopCh)
		if s.term != nil {
			s.term.PostEvent(terminal.Event{Type: terminal.EventClosed})
		}
		<-s.doneCh
	}
	s.finiOnce.Do(func() {
		if s.term != nil {
			s.term.Fini()
		}
	})
}

func (s *TerminalService) Terminal() terminal.Terminal   { return s.term }
func (s *TerminalService) Events() <-chan terminal.Event { return s.eventCh }
