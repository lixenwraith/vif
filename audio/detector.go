package audio

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// backendSpecs returns the priority-ordered candidate table
// pacat retained first: empirically the reliable route on desktop PipeWire
// via pulse-compat; probe verification makes a missing or instantly-dying
// entry fall through to the next candidate
func backendSpecs() []*BackendConfig {
	return []*BackendConfig{
		{Type: BackendPulse, Name: "pacat", Args: []string{"--raw", "--format=s16le", "--rate=44100", "--channels=2", "--latency-msec=50", "--playback"}},
		{Type: BackendPipeWire, Name: "pw-cat", Args: []string{"--playback", "--format=s16", "--rate=44100", "--channels=2", "--latency=50ms", "-"}},
		{Type: BackendALSA, Name: "aplay", Args: []string{"-t", "raw", "-f", "S16_LE", "-r", "44100", "-c", "2", "-q"}},
		{Type: BackendSoX, Name: "sox", Args: []string{"-t", "raw", "-e", "signed", "-b", "16", "-c", "2", "-r", "44100", "-", "-d", "-q"}},
		{Type: BackendFFplay, Name: "ffplay", Args: []string{"-nodisp", "-autoexit", "-f", "s16le", "-ac", "2", "-ar", "44100", "-probesize", "32", "-analyzeduration", "0", "-i", "pipe:0", "-loglevel", "quiet"}},
		{Type: BackendOSS, Name: "oss", Path: "/dev/dsp"},
	}
}

// lookupBin maps backend name to executable name
func lookupBin(name string) string {
	if name == "sox" {
		return "play"
	}
	return name
}

// DetectBackends returns installed candidates in priority order
// force restricts selection to a single named backend ("" = all)
// Returns ErrNoAudioBackend when nothing is installed; caller drops or logs
// it — silent mode remains the degradation path
func DetectBackends(force string) ([]*BackendConfig, error) {
	var out []*BackendConfig
	for _, spec := range backendSpecs() {
		if force != "" && spec.Name != force {
			continue
		}
		if spec.Type == BackendOSS {
			if runtime.GOOS == "freebsd" {
				if _, err := os.Stat(spec.Path); err == nil {
					out = append(out, spec)
				}
			}
			continue
		}
		if path, err := exec.LookPath(lookupBin(spec.Name)); err == nil {
			spec.Path = path
			out = append(out, spec)
		}
	}
	if len(out) == 0 {
		if force != "" {
			return nil, fmt.Errorf("%w: forced backend %q not installed", ErrNoAudioBackend, force)
		}
		return nil, ErrNoAudioBackend
	}
	return out, nil
}

