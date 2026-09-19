//go:build !vif_headless && !vif_noaudio && !wasm

package resource

import (
	"fmt"
	"io"
	"os"

	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

func checkAudio(o Options, w io.Writer) error {
	src, err := Audio(o)
	if err != nil {
		return err
	}
	if src.MusicPath == "" && src.SoundPath == "" {
		fmt.Fprintln(w, "audio ok: embedded defaults")
		return nil
	}
	if src.MusicPath != "" {
		data, err := os.ReadFile(src.MusicPath)
		if err != nil {
			return fmt.Errorf("music %s: %w", src.MusicPath, err)
		}
		if _, err := audio.LoadPatternsTOML(data); err != nil {
			return fmt.Errorf("music %s: %w", src.MusicPath, err)
		}
		fmt.Fprintln(w, "music ok:", src.MusicPath)
	}
	if src.SoundPath != "" {
		data, err := os.ReadFile(src.SoundPath)
		if err != nil {
			return fmt.Errorf("sounds %s: %w", src.SoundPath, err)
		}
		if _, err := audio.LoadSoundsTOML(data); err != nil {
			return fmt.Errorf("sounds %s: %w", src.SoundPath, err)
		}
		fmt.Fprintln(w, "sounds ok:", src.SoundPath)
	}
	return nil
}
