// @focus: #sys { audio }
package audio

import (
	"os"
	"os/exec"
	"runtime"
)

// DetectBackend searches for available audio backends
// Priority: pacat > pw-cat > aplay > play (sox) > ffplay > OSS
func DetectBackend() (*BackendConfig, error) {
	// PulseAudio/PipeWire (works on Linux and FreeBSD with pulse installed)
	if path, err := exec.LookPath("pacat"); err == nil {
		return &BackendConfig{
			Type: BackendPulse,
			Name: "pacat",
			Path: path,
			Args: []string{
				"--raw",
				"--format=s16le",
				"--rate=44100",
				"--channels=2",
				"--latency-msec=50",
				"--playback",
			},
		}, nil
	}

	// PipeWire native
	if path, err := exec.LookPath("pw-cat"); err == nil {
		return &BackendConfig{
			Type: BackendPipeWire,
			Name: "pw-cat",
			Path: path,
			Args: []string{
				"--playback",
				"--format=s16",
				"--rate=44100",
				"--channels=2",
				"--latency=50ms",
				"-",
			},
		}, nil
	}

	// ALSA (Linux)
	if path, err := exec.LookPath("aplay"); err == nil {
		return &BackendConfig{
			Type: BackendALSA,
			Name: "aplay",
			Path: path,
			Args: []string{
				"-t", "raw",
				"-f", "S16_LE",
				"-r", "44100",
				"-c", "2",
				"-q",
			},
		}, nil
	}

	// SoX (cross-platform)
	if path, err := exec.LookPath("play"); err == nil {
		return &BackendConfig{
			Type: BackendSoX,
			Name: "sox",
			Path: path,
			Args: []string{
				"-t", "raw",
				"-e", "signed",
				"-b", "16",
				"-c", "2",
				"-r", "44100",
				"-",
				"-d",
				"-q",
			},
		}, nil
	}

	// FFplay (heavyweight fallback)
	if path, err := exec.LookPath("ffplay"); err == nil {
		return &BackendConfig{
			Type: BackendFFplay,
			Name: "ffplay",
			Path: path,
			Args: []string{
				"-nodisp",
				"-autoexit",
				"-f", "s16le",
				"-ac", "2",
				"-ar", "44100",
				"-probesize", "32",
				"-analyzeduration", "0",
				"-i", "pipe:0",
				"-loglevel", "quiet",
			},
		}, nil
	}

	// FreeBSD OSS (direct device write, no exec needed)
	if runtime.GOOS == "freebsd" {
		if _, err := os.Stat("/dev/dsp"); err == nil {
			return &BackendConfig{
				Type: BackendOSS,
				Name: "oss",
				Path: "/dev/dsp",
				Args: nil, // Direct file write
			}, nil
		}
	}

	return nil, ErrNoAudioBackend
}