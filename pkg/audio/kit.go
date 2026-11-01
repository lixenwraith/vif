package audio

// kit binds the instruments to what they play: each drum to its rendered variant
// set, aliasing the cache so there is one render pass and no copy, and each tonal
// instrument to its voice. Voices read through it at trigger time, which is what
// lets a hot-reload reach every voice in every PatternPlayer.
type kit struct {
	ids      [InstrumentCount]SoundID
	variants [InstrumentCount][]floatBuffer
	voices   []*VoiceDef // by InstrumentType; a drum's and an unset voice's nil
	tuning   float64     // the ratio every tonal note's frequency is scaled by; zero reads as one, A440
}

// ratio is the tuning's frequency ratio, one when unset
func (k *kit) ratio() float64 {
	if k.tuning <= 0 {
		return 1
	}
	return k.tuning
}

var drumSoundNames = [InstrumentCount]string{
	InstrKick:  "kick",
	InstrSnare: "snare",
	InstrHihat: "hihat",
	InstrClap:  "clap",
}

func buildKit(c *soundCache) *kit {
	k := &kit{voices: voiceTable()}
	for i := InstrumentType(0); i <= InstrClap; i++ {
		id := SoundIDByName(drumSoundNames[i])
		k.ids[i] = id
		k.variants[i] = c.variants(id)
	}
	return k
}

// rebind points the kit at a re-rendered set. Mix-goroutine confined. Voices
// already ringing hold their own buffer alias and complete on the old take;
// the next trigger picks up the new one.
func (k *kit) rebind(id SoundID, bufs []floatBuffer) {
	if id == SoundNone {
		return
	}
	for i := range k.ids {
		if k.ids[i] == id {
			k.variants[i] = bufs
		}
	}
}

// voice is the instrument's voice, the default sine for one no voice defines; a
// nil kit, which tests play on, has none.
func (k *kit) voice(i InstrumentType) *VoiceDef {
	if k != nil && i >= 0 && int(i) < len(k.voices) && k.voices[i] != nil {
		return k.voices[i]
	}
	return &defaultVoice
}

// setVoice installs a voice, growing the table for an instrument a document
// added. Mix-goroutine confined; a note already sounding keeps the spec it began on.
func (k *kit) setVoice(i InstrumentType, d *VoiceDef) {
	if i < 0 {
		return
	}
	for int(i) >= len(k.voices) {
		k.voices = append(k.voices, nil)
	}
	k.voices[i] = d
}
