package audio

import (
	"errors"
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