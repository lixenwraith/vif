package audio

import "github.com/lixenwraith/vi-fighter/core"

// drumKit holds pre-rendered percussion buffers, N variants per instrument
// Built once at engine Start; read-only afterward — shared without locks (D3)
type drumKit struct {
	variants [core.InstrumentCount][]floatBuffer
}

// buildDrumKit renders n variants of each drum
// Deterministic parameter walk per variant index; noise content differs per
// render pass, giving natural hit-to-hit variation without runtime DSP (#6)
func buildDrumKit(n int) *drumKit {
	if n < 1 {
		n = 1
	}
	k := &drumKit{}
	for i := 0; i < n; i++ {
		// Spread: ±4% pitch, ±10% decay across the variant range
		det := 1.0 + 0.08*(float64(i)/float64(n)-0.5) // 0.96..1.04
		dec := 1.0 + 0.20*(float64(i)/float64(n)-0.5) // 0.90..1.10
		k.variants[core.InstrKick] = append(k.variants[core.InstrKick], generateKickVar(det, dec))
		k.variants[core.InstrHihat] = append(k.variants[core.InstrHihat], generateHihatVar(dec))
		k.variants[core.InstrSnare] = append(k.variants[core.InstrSnare], generateSnareVar(det, dec))
		k.variants[core.InstrClap] = append(k.variants[core.InstrClap], generateClapVar(dec))
	}
	return k
}
