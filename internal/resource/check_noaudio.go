//go:build vif_headless || vif_noaudio || wasm

package resource

import (
	"errors"
	"fmt"
	"io"
)

func checkAudio(o Options, w io.Writer) error {
	if o.Music != "" || o.Sounds != "" {
		return errors.New("audio overrides are unavailable in this build")
	}
	fmt.Fprintln(w, "audio omitted: unavailable in this build")
	return nil
}
