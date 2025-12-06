//go:build unix

package terminal

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"golang.org/x/sys/unix"
)

// ResizeEvent represents a terminal resize
type ResizeEvent struct {
	Width  int
	Height int
}

// resizeHandler manages SIGWINCH signals
type resizeHandler struct {
	fd      int
	sigCh   chan os.Signal
	eventCh chan ResizeEvent
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// newResizeHandler creates a resize handler for the given fd
func newResizeHandler(fd int) *resizeHandler {
	return &resizeHandler{
		fd:      fd,
		sigCh:   make(chan os.Signal, 1),
		eventCh: make(chan ResizeEvent, 1),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// start begins listening for SIGWINCH
func (r *resizeHandler) start() {
	signal.Notify(r.sigCh, syscall.SIGWINCH)
	go r.watchLoop()
}

// stop stops the resize handler
func (r *resizeHandler) stop() {
	signal.Stop(r.sigCh)
	close(r.stopCh)
	<-r.doneCh
}

// events returns the resize event channel
func (r *resizeHandler) events() <-chan ResizeEvent {
	return r.eventCh
}

// watchLoop monitors for resize signals
func (r *resizeHandler) watchLoop() {
	defer close(r.doneCh)

	// TODO: attempt bubbling it up instead of adding terminal dependency
	// Panic recovery for resize signal handler
	defer func() {
		if r := recover(); r != nil {
			EmergencyReset(os.Stdout)
			fmt.Fprintf(os.Stderr, "\n\x1b[31mRESIZE HANDLER CRASHED: %v\x1b[0m\n", r)
			fmt.Fprintf(os.Stderr, "Stack Trace:\n%s\n", debug.Stack())
			os.Exit(1)
		}
	}()

	for {
		select {
		case <-r.stopCh:
			return
		case <-r.sigCh:
			w, h := r.getSize()
			if w > 0 && h > 0 {
				// Non-blocking send, drop old event if not consumed
				select {
				case r.eventCh <- ResizeEvent{Width: w, Height: h}:
				default:
					// Replace old event
					select {
					case <-r.eventCh:
					default:
					}
					r.eventCh <- ResizeEvent{Width: w, Height: h}
				}
			}
		}
	}
}

// getSize returns current terminal dimensions
func (r *resizeHandler) getSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(r.fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

// getTerminalSize returns the terminal size for a given fd
func getTerminalSize(fd int) (int, int) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 80, 24 // Fallback
	}
	return int(ws.Col), int(ws.Row)
}