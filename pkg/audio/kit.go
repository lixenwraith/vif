package audio

import "sync"

// kit binds the instruments to what they play: each drum to its rendered takes,
// the full-velocity layer aliasing the cache, and each tonal instrument to its
// voice. Voices read through it at trigger time, which is what lets a hot-reload
// reach every voice in every PatternPlayer.
type kit struct {
	ids    [InstrumentCount]SoundID
	takes  [InstrumentCount]drumTakes
	voices []*VoiceDef // by InstrumentType; a drum's and an unset voice's nil
	tuning float64     // the ratio every tonal note's frequency is scaled by; zero reads as one, A440
	only   []bool      // by InstrumentType, the instruments that sound; nil for all
}

// drumTakes is a drum's rendered set: its variants once a velocity layer, the
// layers ascending as their velocities
type drumTakes struct {
	vel    []float64
	layers [][]floatBuffer
}

// take is variant n of the drum's layer nearest vel, nil when it has none: a
// harder hit plays a brighter take as well as a louder one
func (k *kit) take(i InstrumentType, vel float64, n uint32) floatBuffer {
	t := &k.takes[i]
	l := 0
	for l+1 < len(t.layers) && vel > (t.vel[l]+t.vel[l+1])/2 {
		l++
	}
	if l >= len(t.layers) || len(t.layers[l]) == 0 {
		return nil
	}
	return t.layers[l][n%uint32(len(t.layers[l]))]
}

// sounds says whether an instrument plays on this kit: every one unless only
// names some, as a render of one line of a song does
func (k *kit) sounds(i InstrumentType) bool {
	return k == nil || k.only == nil || (i >= 0 && int(i) < len(k.only) && k.only[i])
}

// ratio is the tuning's frequency ratio, one when unset and for the nil kit
// tests play on
func (k *kit) ratio() float64 {
	if k == nil || k.tuning <= 0 {
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

// buildKit renders the drums' velocity layers from the frozen registry, under
// the shapes the cache was rendered with, a drum a goroutine
func buildKit(c *soundCache, shapes map[string]SoundShape) *kit {
	k := &kit{voices: voiceTable()}
	var wg sync.WaitGroup
	for i := InstrumentType(0); i <= InstrClap; i++ {
		id := SoundIDByName(drumSoundNames[i])
		k.ids[i] = id
		if d := SoundDefByName(drumSoundNames[i]); d != nil {
			wg.Go(func() { k.takes[i] = renderTakes(d, shapes[d.Name], c.variants(id)) })
		}
	}
	wg.Wait()
	return k
}

// offlineDrum is a drum's takes kept from one offline render for the next, good
// while the registry holds the same definition under the same shape
type offlineDrum struct {
	d     *SoundDef
	shape SoundShape
	takes drumTakes
}

// offlineTakes is a drum's takes for an offline render, rendered again only when
// its definition or shape changed: a corpus renders thousands of loops on one kit
func (ae *AudioEngine) offlineTakes(i InstrumentType, d *SoundDef, shape SoundShape) drumTakes {
	ae.kitMu.Lock()
	defer ae.kitMu.Unlock()
	if o := &ae.offline[i]; o.d != d || o.shape != shape {
		*o = offlineDrum{d: d, shape: shape, takes: renderTakes(d, shape, nil)}
	}
	return ae.offline[i].takes
}

// rebind points the kit at a re-rendered set. Mix-goroutine confined. Voices
// already ringing hold their own buffer alias and complete on the old take;
// the next trigger picks up the new one.
func (k *kit) rebind(id SoundID, t drumTakes) {
	if id == SoundNone {
		return
	}
	for i := range k.ids {
		if k.ids[i] == id {
			k.takes[i] = t
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
