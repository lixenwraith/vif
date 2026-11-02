//go:build !vif_headless && !vif_noaudio && !wasm

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/soundlab"
)

// CLI flags, matching the game binary's house style; the -config flags are vif's.
var (
	flagBackend   = flag.String("ab", "", "Force audio backend by name (pacat, aplay, null, wav:out.wav, ...)")
	flagHeadless  = flag.Bool("headless", false, "Discard audio output (equivalent to -ab null)")
	flagScript    = flag.String("s", "", "Execute script file and exit")
	flagConfigDir = flag.String("config-dir", "", "Configuration root holding audio/ (as vif)")
	flagMusic     = flag.String("config-music", "", "Music pattern override TOML (as vif)")
	flagSounds    = flag.String("config-sounds", "", "Sound definition override TOML (as vif)")
	flagVol       = flag.Float64("vol", 0.7, "Sound effect volume 0..1; music keeps its own level")
	flagTUI       = flag.Bool("tui", false, "Full-screen TUI (default: line REPL)")
)

func main() {
	flag.BoolVar(flagTUI, "T", false, "Full-screen TUI, as -tui")
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	backend := *flagBackend
	if *flagHeadless {
		backend = "null"
	}
	files, err := soundlab.ResolveFiles(*flagConfigDir, *flagMusic, *flagSounds)
	if err != nil {
		return err
	}
	// The TUI's log takes what the session says from the start, which stdout would
	// show only after the full screen closes
	tui := *flagTUI && *flagScript == ""
	var out io.Writer = os.Stdout
	lb := soundlab.NewLogBuffer(500)
	if tui {
		out = lb
	}
	s, err := soundlab.NewSession(soundlab.Options{Backend: backend, SfxVolume: *flagVol, Out: out, Files: files})
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.StartError(); err != nil {
		fmt.Fprintf(out, "audio backend: %v (silent mode; edit/validate/export still work)\n", err)
	}

	if *flagScript != "" {
		return soundlab.RunScriptFile(s, *flagScript)
	}
	if tui {
		return runTUI(s, lb)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; s.Close(); os.Exit(130) }()
	runREPL(s, os.Stdin, os.Stdout)
	return nil
}

// runTUI runs the TUI on this process's terminal. A signal becomes a clean loop
// exit rather than os.Exit, so the terminal is never left raw on the alternate
// screen, and Close's warning reaches stdout once the screen is gone.
func runTUI(s *soundlab.Session, lb *soundlab.LogBuffer) error {
	defer s.SetOut(os.Stdout)
	term := terminal.New()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sig:
			term.PostEvent(terminal.Event{Type: terminal.EventClosed})
		case <-done:
		}
	}()
	err := soundlab.RunTUI(s, lb, term)
	if err != nil {
		for _, l := range lb.Lines() { // what the session said before the screen failed to open
			fmt.Println(l)
		}
	}
	return err
}
