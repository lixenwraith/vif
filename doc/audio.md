# Audio Package — Design Document

## Overview

Pure Go PCM audio synthesis library with zero CGO dependencies. Generates waveforms in memory and pipes to system audio tools via `os/exec`. Gracefully degrades to silent mode when no backend is available.

**Target Platforms:** Linux (PulseAudio, PipeWire, ALSA), FreeBSD (PulseAudio, OSS). macOS not supported.

---

## Architecture
```
┌─────────────────────────────────────────────────────────────────┐
│                        AudioEngine                              │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Public Interface                      │   │
│  │  Start() Stop() Play() ToggleMute() SetVolume() ...      │   │
│  └──────────────────────────────────────────────────────────┘   │
│         │                    │                    │             │
│         ▼                    ▼                    ▼             │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐        │
│  │   Mixer     │     │ SoundCache  │     │  Detector   │        │
│  │             │     │             │     │             │        │
│  │ Float mix   │     │ Lazy gen    │     │ Backend     │        │
│  │ Soft limit  │     │ Unity gain  │     │ probe       │        │
│  │ PCM output  │     │ floatBuffer │     │             │        │
│  └──────┬──────┘     └─────────────┘     └─────────────┘        │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    Output Layer                         │    │
│  │  exec.Cmd stdin (pacat/aplay/etc)  │  /dev/dsp (OSS)    │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | File | Role |
|-----------|------|------|
| `AudioEngine` | engine.go | Lifecycle, public API, process management |
| `Mixer` | mixer.go | Sound queue, float mixing, PCM output |
| `soundCache` | cache.go | Lazy generation, buffer storage |
| `DetectBackend` | detector.go | System tool probing |
| Generators | generators.go | Waveform synthesis, envelope shaping |

---

## Data Structures

### SoundType
```go
type SoundType int

const (
    SoundError  SoundType = iota  // Typing error buzz
    SoundBell                     // Nugget collection
    SoundWhoosh                   // Cleaner activation
    SoundCoin                     // Gold sequence complete
)
```

### BackendConfig
```go
type BackendConfig struct {
    Type BackendType  // BackendPulse, BackendALSA, etc.
    Name string       // Human-readable identifier
    Path string       // Binary path or device path
    Args []string     // Command-line arguments
}
```

### AudioConfig
```go
type AudioConfig struct {
    Enabled       bool
    MasterVolume  float64                // 0.0 to 1.0
    EffectVolumes map[SoundType]float64  // Per-sound multipliers
    SampleRate    int                    // Default 44100
}
```

### floatBuffer
```go
type floatBuffer []float64
```

Mono float64 samples at unity gain (-1.0 to 1.0). Volume applied at mix time, enabling real-time volume adjustment without cache invalidation.

---

## Backend Detection

### Priority Order

| Priority | Backend | Platform | Detection |
|----------|---------|----------|-----------|
| 1 | `pacat` | Linux/BSD+Pulse | `exec.LookPath` |
| 2 | `pw-cat` | PipeWire | `exec.LookPath` |
| 3 | `aplay` | Linux ALSA | `exec.LookPath` |
| 4 | `play` | SoX | `exec.LookPath` |
| 5 | `ffplay` | FFmpeg | `exec.LookPath` |
| 6 | `/dev/dsp` | FreeBSD OSS | `os.Stat` |

### Backend Arguments
```
pacat:   --raw --format=s16le --rate=44100 --channels=2 --latency-msec=50 --playback
pw-cat:  --playback --format=s16 --rate=44100 --channels=2 --latency=50ms -
aplay:   -t raw -f S16_LE -r 44100 -c 2 -q
play:    -t raw -e signed -b 16 -c 2 -r 44100 - -d -q
ffplay:  -nodisp -autoexit -f s16le -ac 2 -ar 44100 -probesize 32 -analyzeduration 0 -i pipe:0 -loglevel quiet
OSS:     Direct file write to /dev/dsp (no exec)
```

### Failure Handling

If all probes fail, `DetectBackend()` returns `ErrNoAudioBackend`. Engine enters silent mode—all `Play()` calls become no-ops. Game continues without audio.

---

## Sound Generation

### Waveform Types

| Type | Formula | Use Case |
|------|---------|----------|
| Sine | `sin(2π × phase)` | Bell tones |
| Square | `phase < 0.5 ? 1 : -1` | Coin chime |
| Saw | `2 × (phase - 0.5)` | Error buzz |
| Noise | `rand() × 2 - 1` | Whoosh |

### Oscillator
```go
func oscillator(waveType int, freq float64, samples int) floatBuffer
```

Generates raw waveform at specified frequency. Phase accumulator maintains continuity.

### ADSR Envelope

Simplified attack/release envelope (no decay/sustain segments):
```
     /\
    /  \___________
   /               \
  /                 \
 A        S          R
```
```go
func applyEnvelope(buf floatBuffer, attackSec, releaseSec float64)
```

Modifies buffer in-place. Linear ramps for attack and release phases.

### Sound Definitions

| Sound | Duration | Waveform | Frequency | Notes |
|-------|----------|----------|-----------|-------|
| Error | 80ms | Saw | 100Hz | 5ms attack, 20ms release |
| Bell | 600ms | Sine×2 | 880Hz + 1760Hz | Mixed 70/30, separate envelopes |
| Whoosh | 300ms | Noise | — | 150ms attack, 150ms release |
| Coin | 360ms | Square×2 | 987Hz → 1318Hz | Sequential notes (B5, E6) |

---

## Mixing Pipeline

### Architecture
```
Play(SoundType)
       │
       ▼
┌─────────────┐
│ playQueue   │  chan playRequest (cap 32)
│ (buffered)  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Mixer Loop  │  50ms tick (aligned with game tick)
│             │
│ 1. Drain    │  Process queued requests
│ 2. Mix      │  Sum active sounds (float64)
│ 3. Limit    │  Soft limiter → hard clip
│ 4. Convert  │  float64 → int16 LE stereo
│ 5. Write    │  Pipe to backend
└─────────────┘
```

### Float Mixing

All active sounds summed in float64 space:
```go
for j := 0; j < samples && r.pos < len(r.buffer); j++ {
    buf[j] += r.buffer[r.pos] * r.volume
    r.pos++
}
```

Prevents integer overflow during polyphonic mixing.

### Soft Limiter

Tanh-style knee starting at ±0.8:
```go
if v > 0.8 {
    v = 0.8 + 0.2*(1.0 - 1.0/(1.0+(v-0.8)*5.0))
}
```

Provides ~4dB of headroom before hard clip. Produces musical saturation rather than harsh digital clipping.

### Output Format

- Sample Rate: 44100 Hz
- Channels: 2 (stereo, duplicated mono)
- Bit Depth: 16-bit signed
- Byte Order: Little Endian
- Frame Size: 4 bytes (2 channels × 2 bytes)

### Silence Padding

When no sounds active, mixer writes zero-filled buffers. Prevents backend pipe closure due to underrun.

---

## Sound Cache

### Strategy

| Sound | Strategy | Rationale |
|-------|----------|-----------|
| Error | Preloaded | Most frequent (typing errors) |
| Bell | Lazy | Infrequent (nugget collection) |
| Whoosh | Lazy | Infrequent (cleaner activation) |
| Coin | Lazy | Rare (gold completion) |

### Implementation
```go
type soundCache struct {
    mu    sync.RWMutex
    store [soundTypeCount]floatBuffer
    ready [soundTypeCount]bool
}
```

Fixed-size array indexed by `SoundType`. Double-checked locking pattern for thread-safe lazy initialization.

### Cache Characteristics

- **No eviction:** Only 4 sound types, bounded memory (~120KB total)
- **Unity gain storage:** Volume applied at mix time
- **No invalidation:** Volume changes don't require regeneration

---

## Engine Lifecycle

### Initialization
```
NewAudioEngine()
       │
       ├─► Create config (default or provided)
       ├─► Create sound cache
       ├─► Preload Error sound
       └─► Return engine (not started)
```

### Start Sequence
```
Start()
   │
   ├─► DetectBackend()
   │      │
   │      ├─► Success: Configure backend
   │      └─► Failure: Set silentMode, return nil
   │
   ├─► Open output (exec.Cmd or /dev/dsp)
   │      │
   │      └─► Failure: Set silentMode, return nil
   │
   ├─► Start Mixer goroutine
   ├─► Start monitorProcess goroutine (exec backends)
   ├─► Start monitorMixer goroutine
   └─► Set running = true
```

### Stop Sequence
```
Stop()
   │
   ├─► Set running = false (CAS)
   ├─► mixer.Stop() (close stopChan)
   ├─► Close stdin pipe
   ├─► Close OSS file handle
   ├─► Kill subprocess
   └─► wg.Wait() (join monitors)
```

### Error Recovery

| Failure | Detection | Response |
|---------|-----------|----------|
| Backend missing | `DetectBackend()` | Silent mode |
| Spawn failure | `cmd.Start()` | Silent mode |
| Pipe break | `Write()` error | Silent mode via errChan |
| Process exit | `cmd.Wait()` | Silent mode |

All failures result in silent mode—game continues without crashing.

---

## Thread Safety

### Synchronization Primitives

| Resource | Protection | Access Pattern |
|----------|------------|----------------|
| `config` | `sync.RWMutex` | Read-heavy (Play), rare write (SetVolume) |
| `running` | `atomic.Bool` | Multiple readers, single writer |
| `muted` | `atomic.Bool` | Toggle from any goroutine |
| `silentMode` | `atomic.Bool` | Set once on failure |
| `cache` | `sync.RWMutex` | Read-heavy after warmup |
| `mixer.stats` | `sync.Mutex` | Increment only |
| `mixer.active` | None | Single-writer (mixer goroutine) |

### Goroutines

| Goroutine | Lifetime | Purpose |
|-----------|----------|---------|
| `mixer.loop` | Start → Stop | Mix and output |
| `monitorProcess` | Start → process exit | Detect subprocess death |
| `monitorMixer` | Start → mixer stop | Propagate pipe errors |

---

## Performance Characteristics

### Memory

| Allocation | Size | Frequency |
|------------|------|-----------|
| Error buffer | ~7KB | Once (preload) |
| Bell buffer | ~53KB | Once (first play) |
| Whoosh buffer | ~26KB | Once (first play) |
| Coin buffer | ~32KB | Once (first play) |
| Mix buffer | ~18KB | Once (mixer init) |
| Output buffer | ~9KB | Once (mixer init) |

Total steady-state: ~145KB

### CPU

| Operation | Cost |
|-----------|------|
| Sound generation | O(n) samples, one-time |
| Mix per tick | O(active × samples) |
| Soft limiter | O(samples) |
| Output conversion | O(samples) |

### Latency

| Component | Contribution |
|-----------|--------------|
| Buffer duration | 50ms |
| Backend latency | ~50ms (configurable) |
| **Total** | ~100ms end-to-end |

### Queue Behavior

- Capacity: 32 requests
- Overflow: Drop newest, increment `dropped` counter
- Drain: Up to 4 additional per tick

---

## API Reference

### Core Methods
```go
func NewAudioEngine(cfg ...*AudioConfig) (*AudioEngine, error)
func (ae *AudioEngine) Start() error
func (ae *AudioEngine) Stop()
func (ae *AudioEngine) Play(st SoundType) bool
```

### Volume Control
```go
func (ae *AudioEngine) SetVolume(vol float64)      // 0.0-1.0
func (ae *AudioEngine) ToggleMute() bool           // Returns isEnabled
func (ae *AudioEngine) IsMuted() bool
```

### Status
```go
func (ae *AudioEngine) IsEnabled() bool            // Running && !Muted && !Silent
func (ae *AudioEngine) IsRunning() bool            // Started (may be silent)
func (ae *AudioEngine) GetStats() (played, dropped, overflow uint64)
```

### Compatibility Shims
```go
func (ae *AudioEngine) SendRealTime(cmd AudioCommand) bool  // Maps to Play()
func (ae *AudioEngine) SendState(cmd AudioCommand) bool     // Maps to Play()
func (ae *AudioEngine) DrainQueues()                        // No-op
func (ae *AudioEngine) StopCurrentSound()                   // No-op
```

---

## Extension Points

### Adding New Sounds

1. Add constant to `SoundType` enum (before `soundTypeCount`)
2. Add generator case in `generateSound()`
3. Add default volume in `DefaultAudioConfig().EffectVolumes`
4. Optionally preload in `soundCache.preload()`

### Adding New Backends

1. Add constant to `BackendType` enum
2. Add detection block in `DetectBackend()` with appropriate priority
3. For exec backends: specify path and args
4. For direct I/O: add handling in `Start()` output setup

### Custom Waveforms

Add new waveform type constant and case in `oscillator()`:
```go
case waveCustom:
    buf[i] = customFormula(phase)
```

---

## Known Limitations

| Limitation | Rationale |
|------------|-----------|
| No macOS support | No testable backend available |
| No Windows support | Fundamentally different audio API |
| ~100ms latency | Buffer size tradeoff for stability |
| Mono to stereo duplication | Game has no spatial audio |
| No streaming/music | Designed for short sound effects |
| No real-time parameter modulation | Complexity vs. use case |

---

## Error Handling

### Sentinel Errors
```go
var (
    ErrNoAudioBackend = errors.New("no compatible audio backend found")
    ErrPipeClosed     = errors.New("audio pipe closed")
)
```

### Error Wrapping

Pipe write errors wrapped with sentinel:
```go
fmt.Errorf("%w: %v", ErrPipeClosed, err)
```

Enables `errors.Is(err, ErrPipeClosed)` checking.

---

## File Reference

| File | Lines | Purpose |
|------|-------|---------|
| types.go | ~45 | SoundType, BackendType, BackendConfig, errors |
| config.go | ~25 | AudioConfig, DefaultAudioConfig() |
| detector.go | ~70 | DetectBackend(), backend probe chain |
| generators.go | ~90 | Waveform synthesis, envelope, sound factories |
| cache.go | ~40 | soundCache, lazy generation, preload |
| mixer.go | ~130 | Mixer, float mixing, soft limiter, PCM output |
| engine.go | ~150 | AudioEngine, lifecycle, public API |

**Total:** ~550 lines (excluding constants)

---

## Constants Reference
```go
// Hardware
AudioSampleRate    = 44100
AudioChannels      = 2
AudioBitDepth      = 16
AudioBytesPerFrame = 4

// Timing
AudioBufferDuration = 50ms
AudioBufferSamples  = 2205
MinSoundGap         = 50ms
```

Sound timing constants defined in `constants/audio.go`.