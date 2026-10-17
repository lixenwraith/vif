//go:build !vif_headless && !vif_noaudio

package resource

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vif/pkg/audio"
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
	if p := src.SoundPath; p != "" {
		if _, err := audio.LoadSoundsFS(os.DirFS(filepath.Dir(p)), filepath.Base(p)); err != nil {
			return fmt.Errorf("sounds %s: %w", p, err)
		}
		fmt.Fprintln(w, "sounds ok:", p)
	}
	return nil
}
