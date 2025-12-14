package audio

import (
	"errors"
)

// SoundType represents different sound effects
type SoundType int

const (
	SoundError  SoundType = iota // Typing error buzz
	SoundBell                    // Nugget collection
	SoundWhoosh                  // Cleaner activation
	SoundCoin                    // Gold sequence complete
	soundTypeCount
)

// BackendType identifies the audio backend
type BackendType int

const (
	BackendPulse BackendType = iota
	BackendPipeWire
	BackendALSA
	BackendSoX
	BackendFFplay
	BackendOSS
)

// BackendConfig describes a CLI audio backend
type BackendConfig struct {
	Type BackendType
	Name string
	Path string
	Args []string
}

// Sentinel errors
var (
	ErrNoAudioBackend = errors.New("no compatible audio backend found")
	ErrPipeClosed     = errors.New("audio pipe closed")
)