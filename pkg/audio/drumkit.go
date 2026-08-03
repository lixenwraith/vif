package audio

// drumKit binds InstrumentType to rendered variant sets from the sound
// registry. Buffers alias the cache, so there is one render pass and no copy.
// DrumVoice reads through this struct at trigger time, which is what lets a
// hot-reload reach every voice in every PatternPlayer.
type drumKit struct {
	ids      [InstrumentCount]SoundID
	variants [InstrumentCount][]floatBuffer
}

var drumSoundNames = [InstrumentCount]string{
	InstrKick:  "kick",
	InstrSnare: "snare",
	InstrHihat: "hihat",
	InstrClap:  "clap",
}

func buildDrumKit(c *soundCache) *drumKit {
	k := &drumKit{}
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
func (k *drumKit) rebind(id SoundID, bufs []floatBuffer) {
	if id == SoundNone {
		return
	}
	for i := range k.ids {
		if k.ids[i] == id {
			k.variants[i] = bufs
		}
	}
}
