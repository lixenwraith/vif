package audio

// drumKit binds InstrumentType to rendered variant sets from the sound
// registry. Drum timbre lives in builtin/drums.toml like any other sound;
// buffers alias the cache, so there is one render pass and no copy.
type drumKit struct {
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
		k.variants[i] = c.variants(SoundIDByName(drumSoundNames[i]))
	}
	return k
}
