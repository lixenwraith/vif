package core

// MusicIntensity is the conductor's arrangement tier. It is a game concept:
// the audio package has no notion of it and never reads it. Carried in
// EventMusicIntensityChange payloads and mapped to patterns by system/music.go
type MusicIntensity int

const (
	IntensityCalm MusicIntensity = iota
	IntensityNormal
	IntensityElevated
	IntensityIntense
	IntensityPeak
)

func (m MusicIntensity) String() string {
	names := [...]string{"calm", "normal", "elevated", "intense", "peak"}
	if int(m) < len(names) {
		return names[m]
	}
	return "unknown"
}
