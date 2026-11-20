package pattern

import (
	"math"
	"math/bits"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// ArtPreset is a look of stone a rock group takes, its features scaled to the group; a
// plain uint8, so a component or a payload carries one without importing this package
type ArtPreset uint8

const (
	ArtNone    ArtPreset = iota // bare stone
	ArtStrata                   // bands, as many as the group is tall
	ArtCracked                  // veins, as many as the group's area holds
	ArtMoss                     // green over upward faces, thinning with depth
	ArtCrystal                  // sparse bright points, fewer in a small group
	ArtMasonry                  // courses of blocks as long as the group is wide
	ArtPresetCount
)

// FaceReach is the depth from which a cell draws as any deeper one does: a face's
// light, crust and moss reach no further, so a caller measures depth no further
const FaceReach = max(len(visual.StoneLitDepth), len(visual.MossCover), visual.StoneCrust)

// Face is the side of a rock cell its nearest open cell lies on; a floor's top opens up
type Face uint8

const (
	FaceUp Face = iota
	FaceDown
	FaceLeft
	FaceRight
	faceCount
)

// ArtCell is one rock cell and where it lies in its group
type ArtCell struct {
	X, Y  int64 // absolute cell, any int64 such as a cave's profile column: stone moves with its rock
	Depth int   // cells to the nearest open cell, 0 at the face
	Face  Face  // the side that open cell lies on
	// W and H size the group the cell belongs to in columns and rows, its bounding box.
	// Only masonry's block length reads W and only crystals' density reads W×H; bands
	// and veins keep one scale, so their count follows the group's size and a run whose
	// H varies column to column, as a cave's thickness does, stays unbroken.
	W, H int
}

// Each noise source draws its own knots
const (
	channelMottle uint64 = iota + 1
	channelFleck
	channelGrain
	channelBand
	channelWave
	channelVein
	channelMoss
	channelCrystal
	channelCourse
)

// artPresets is each preset's scenario name, its host rock and the features it lays
// over it. A drawer takes the cell by value, so an indirect call cannot move it to the heap.
var artPresets = [ArtPresetCount]struct {
	name    string
	palette *visual.StonePalette
	draw    func(stone) look
}{
	ArtNone:    {"none", &visual.StonePlain, nil},
	ArtStrata:  {"strata", &visual.StonePlain, stone.strata},
	ArtCracked: {"cracked", &visual.StoneCracked, stone.cracked},
	ArtMoss:    {"moss", &visual.StoneMoss, stone.moss},
	ArtCrystal: {"crystal", &visual.StoneCrystal, stone.crystal},
	ArtMasonry: {"masonry", &visual.StoneMasonry, stone.masonry},
}

var faceLight = [faceCount]int64{
	FaceUp: visual.StoneLitUp, FaceDown: visual.StoneLitDown, FaceLeft: visual.StoneLitSide, FaceRight: visual.StoneLitSide,
}

var mossFace = [faceCount]int64{
	FaceUp: visual.StoneUnit, FaceDown: visual.MossDown, FaceLeft: visual.MossSide, FaceRight: visual.MossSide,
}

// String returns the preset's scenario name, or "invalid" out of range
func (p ArtPreset) String() string {
	if p >= ArtPresetCount {
		return "invalid"
	}
	return artPresets[p].name
}

// ParseArtPreset resolves a scenario name; the inverse of ArtPreset.String
func ParseArtPreset(name string) (ArtPreset, bool) {
	for i := range artPresets {
		if artPresets[i].name == name {
			return ArtPreset(i), true
		}
	}
	return ArtNone, false
}

// Art returns the glyph and colours preset p draws cell c in. It is a pure function of
// its arguments in integer arithmetic, so every platform draws the same stone, and it
// allocates nothing, so a renderer may call it for every rock cell of a frame.
func Art(p ArtPreset, seed uint64, c ArtCell) (rune, color.RGB, color.RGB) {
	l := paint(p, seed, c)
	return l.r, l.fg, l.bg
}

// Art256 is Art in xterm-256 indices, every colour but a hued glyph's the grey of its
// luma: a console would turn the rock's muted browns red, so rock reads grey there and
// only moss and crystal points keep a hue
func Art256(p ArtPreset, seed uint64, c ArtCell) (rune, uint8, uint8) {
	l := paint(p, seed, c)
	if !l.hued {
		l.fg = color.Grayscale(l.fg)
	}
	return l.r, color.RGBTo256(l.fg), color.RGBTo256(color.Grayscale(l.bg))
}

// paint draws cell c in preset p: the host rock, the preset's features over it, and the
// light of the face it lies near
func paint(p ArtPreset, seed uint64, c ArtCell) look {
	if p >= ArtPresetCount {
		p = ArtNone
	}
	preset := &artPresets[p]
	s := stone{
		seed: vmath.MixChain(seed), pal: preset.palette,
		x: c.X, y: c.Y, depth: max(int64(c.Depth), 0), face: min(c.Face, FaceRight),
		w: max(int64(c.W), 1), h: max(int64(c.H), 1),
	}
	s.bare()
	if preset.draw != nil {
		s.look = preset.draw(s)
	}
	// A face lifts the cell by depth and by the side it opens on, unevenly as rough rock
	// catches light
	if s.depth < int64(len(visual.StoneLitDepth)) {
		lit := visual.StoneLitDepth[s.depth] * faceLight[s.face] / visual.StoneUnit * (visual.StoneUnit/2 + s.mottle) / visual.StoneUnit
		s.bg = tint(s.bg, lit)
		if !s.lamp {
			s.fg = tint(s.fg, lit)
		}
	}
	return s.look
}

// look is what a cell shows; a lamp's glyph shines by itself, so a face's light leaves it,
// and a hued glyph keeps its hue in 256 colours
type look struct {
	r      rune
	fg, bg color.RGB
	lamp   bool
	hued   bool
}

// stone is one cell as it is drawn: its inputs as int64, the seed's first MixChain round
// every knot resumes from, and its look so far
type stone struct {
	look
	seed   uint64
	pal    *visual.StonePalette
	x, y   int64
	depth  int64
	w, h   int64
	mottle int64  // the host rock's mottle, a share of StoneUnit
	hash   uint64 // the cell's own draws, a share of StoneUnit each
	face   Face
}

// bare draws the host rock: an even grain over two octaves of mottle, each cell a
// little off its neighbours, and a coarser crust along its face as deep as the mottle
// runs high
func (s *stone) bare() {
	mottle := s.noise2(channelMottle, visual.StoneMottleColBits, visual.StoneMottleRowBits)
	fleck := s.noise2(channelFleck, visual.StoneFleckColBits, visual.StoneFleckRowBits)
	s.mottle = mottle + (fleck-mottle)*visual.StoneFleck/visual.StoneUnit
	s.hash = s.knot(channelGrain, s.x, s.y)
	s.bg = color.LerpFixed(s.pal.Bg, s.pal.BgMottle, s.mottle, visual.StoneUnitBits)
	s.fg = tint(color.LerpFixed(s.pal.Fg, s.pal.FgMottle, s.mottle, visual.StoneUnitBits), s.jitter(1, visual.StoneSpeck))
	s.r = visual.StoneGrain
	if s.depth*visual.StoneUnit < visual.StoneCrust*s.mottle {
		s.r = visual.StoneGrainDense
	}
}

// draw is the cell's i-th share of StoneUnit from its own hash
func (s *stone) draw(i uint) int64 {
	return share(s.hash >> (i * visual.StoneUnitBits))
}

// jitter is the cell's i-th draw spread over [-spread, spread)
func (s *stone) jitter(i uint, spread int64) int64 {
	return (s.draw(i) - visual.StoneUnit/2) * 2 * spread / visual.StoneUnit
}

// strataPitch is the bands' spacing in fixed point
const strataPitch = visual.StrataRows * visual.StoneUnit

// strata lays bands StrataRows apart, each boundary straying by its own jitter and the
// whole swelling with a wave along the row; light and dark bands alternate
func (s stone) strata() look {
	wave := s.noise1(channelWave, visual.StrataWaveColBits) - visual.StoneUnit/2
	v := s.y*visual.StoneUnit + visual.StoneUnit/2 + wave*visual.StrataWave/visual.StoneUnit
	k := floorDiv(v, strataPitch)
	h := s.knot(channelBand, k, 0)
	if v < boundary(k, h) {
		k--
		h = s.knot(channelBand, k, 0)
	} else if next := s.knot(channelBand, k+1, 0); v >= boundary(k+1, next) {
		k, h = k+1, next
	}
	h >>= visual.StoneUnitBits
	band := &visual.StrataLight[h%uint64(len(visual.StrataLight))]
	if k%2 != 0 {
		band = &visual.StrataDark[h%uint64(len(visual.StrataDark))]
	}
	shade := (s.mottle - visual.StoneUnit/2) * visual.StrataMottle / visual.StoneUnit
	s.bg, s.fg, s.r = tint(band.Bg, shade), tint(band.Fg, shade+s.jitter(1, visual.StoneSpeck)), band.Grain
	return s.look
}

// boundary is the top of band k, hashed h: k pitches down, give or take its jitter
func boundary(k int64, h uint64) int64 {
	return k*strataPitch + (share(h)-visual.StoneUnit/2)*strataPitch/visual.StoneUnit*visual.StrataJitter/visual.StoneUnit
}

// cracked draws the veins of the tiles about the cell. A vein starts in its tile and
// walks a cell a step along a row or a column, at most a tile along and across; its
// drift at a step is the count of its steps before that went with its trend less those
// that went against, so a cell finds the vein's place in a pair of popcounts.
func (s stone) cracked() look {
	tw, th := int64(visual.CrackTileCols), int64(visual.CrackTileRows)
	tx, ty := floorDiv(s.x, tw), floorDiv(s.y, th)
	for ky := ty - 1; ky <= ty+1; ky++ {
		for kx := tx - 1; kx <= tx+1; kx++ {
			// A vein walks right or down from its start, so none from the tile below and to
			// the right reaches the cell
			if kx > tx && ky > ty {
				break
			}
			if s.vein(s.knot(channelVein, kx, ky), kx*tw, ky*th) {
				return s.look
			}
		}
	}
	return s.look
}

// vein draws the cell if the vein hashed h, from the tile at (left, top), passes it
func (s *stone) vein(h uint64, left, top int64) bool {
	if share(h) >= visual.CrackChance {
		return false
	}
	tw, th := uint64(visual.CrackTileCols), uint64(visual.CrackTileRows)
	d := h >> visual.StoneUnitBits
	sx := left + int64(d%tw)
	d /= tw
	sy := top + int64(d%th)
	d /= th
	along, across := s.x-sx, s.y-sy
	reach, drift := int64(tw), int64(th)
	upright := share(d) < visual.CrackUpright
	if upright {
		along, across = across, along
		reach, drift = drift, reach
	}
	if along < 0 || along >= reach || across > drift || -across > drift {
		return false
	}
	d >>= visual.StoneUnitBits
	n := visual.CrackMinSteps + share(d)*(reach-visual.CrackMinSteps+1)>>visual.StoneUnitBits
	if along >= n {
		return false
	}
	d >>= visual.StoneUnitBits

	// Half the steps hold their line, three in eight go with the trend, one against
	steps := vmath.Mix64(h)
	mask := uint64(1)<<uint(n) - 1
	hold, with, alt := steps&mask, steps>>uint(n)&mask, steps>>uint(2*n)&mask
	forth, back := ^hold&(with|alt)&mask, ^hold&^(with|alt)&mask
	sign := int64(1)
	if d%2 != 0 {
		sign = -1
	}
	before := uint64(1)<<uint(along) - 1
	off := sign * int64(bits.OnesCount64(forth&before)-bits.OnesCount64(back&before))
	step := int64(0)
	if forth>>uint(along)&1 != 0 {
		step = sign
	} else if back>>uint(along)&1 != 0 {
		step = -sign
	}

	// Along a row a vein runs on the cell's foot, so a fall is drawn in the row below
	glyph, at := visual.CrackFlat, off
	switch {
	case upright && step > 0:
		glyph = visual.CrackFall
	case upright && step < 0:
		glyph = visual.CrackRise
	case upright:
		glyph = visual.CrackPlumb
	case step > 0:
		glyph, at = visual.CrackFall, off+1
	case step < 0:
		glyph = visual.CrackRise
	}
	if across != at {
		return false
	}

	// A line inks less of its cell than grain does, so the cell takes the grain's mean
	ink := int64(visual.StoneGrainInk)
	if s.r == visual.StoneGrainDense {
		ink = visual.StoneGrainDenseInk
	}
	fade := visual.CrackEnd + (visual.StoneUnit-visual.CrackEnd)*min(along, n-1-along, visual.CrackTaper)/visual.CrackTaper
	s.bg = color.LerpFixed(s.bg, s.fg, ink, visual.StoneUnitBits)
	s.fg = color.LerpFixed(s.fg, visual.CrackFg, fade, visual.StoneUnitBits)
	s.r = glyph
	return true
}

// moss covers a share of the cells at each depth, most on an upward face, in patches
// from its own noise; thick moss is dense, and sprouts at an upward face
func (s stone) moss() look {
	if s.depth >= int64(len(visual.MossCover)) {
		return s.look
	}
	cover := visual.MossCover[s.depth] * mossFace[s.face] / visual.StoneUnit
	patch := s.noise2(channelMoss, visual.MossColBits, visual.MossRowBits)
	thick := cover - patch - s.jitter(2, visual.MossJitter)
	if thick <= 0 {
		return s.look
	}
	strength := min(thick*visual.StoneUnit/visual.MossThick, visual.StoneUnit)
	s.fg = color.LerpFixed(visual.MossDark, visual.MossLight, strength, visual.StoneUnitBits)
	s.bg = color.LerpFixed(s.bg, visual.MossBg, strength*visual.MossShade/visual.StoneUnit, visual.StoneUnitBits)
	s.r, s.hued = visual.MossThin, true
	if thick >= visual.MossThick {
		s.r = visual.MossDense
		if s.depth == 0 && s.face == FaceUp && s.draw(3) < visual.MossSprouts {
			s.r = visual.MossSprout
		}
	}
	return s.look
}

// A crystal's glow reaches two columns and a row, round on a cell twice as tall
const glowCols, glowRows = 2, 1

// crystal sets at most one point in each tile, far enough in from its edges that its
// glow stays in the tile; a group under CrystalFullArea cells grows proportionally fewer
func (s stone) crystal() look {
	area := min(min(s.w, visual.CrystalFullArea)*min(s.h, visual.CrystalFullArea), visual.CrystalFullArea)
	chance := visual.CrystalChance * area / visual.CrystalFullArea
	tw, th := int64(visual.CrystalTileCols), int64(visual.CrystalTileRows)
	kx, ky := floorDiv(s.x, tw), floorDiv(s.y, th)
	h := s.knot(channelCrystal, kx, ky)
	if share(h) >= chance {
		return s.look
	}
	d := h >> visual.StoneUnitBits
	cx := kx*tw + glowCols + int64(d%uint64(tw-2*glowCols))
	d /= uint64(tw - 2*glowCols)
	cy := ky*th + glowRows + int64(d%uint64(th-2*glowRows))
	d /= uint64(th - 2*glowRows)
	hue := &visual.CrystalHues[s.seed%uint64(len(visual.CrystalHues))]
	dx, dy := s.x-cx, s.y-cy
	switch max(dx, -dx) + glowCols/glowRows*max(dy, -dy) {
	case 0:
		s.r = visual.CrystalGlyphs[d%uint64(len(visual.CrystalGlyphs))]
		s.fg = color.LerpFixed(hue.Dim, hue.Bright, share(d/uint64(len(visual.CrystalGlyphs))), visual.StoneUnitBits)
		s.bg = color.LerpFixed(s.bg, hue.Glow, visual.CrystalCore, visual.StoneUnitBits)
		s.lamp, s.hued = true, true
	case 1:
		s.bg = color.LerpFixed(s.bg, hue.Glow, visual.CrystalHalo, visual.StoneUnitBits)
	case 2:
		s.bg = color.LerpFixed(s.bg, hue.Glow, visual.CrystalHalo/2, visual.StoneUnitBits)
	}
	return s.look
}

// masonry lays courses a row high of blocks as long as the group is wide over
// MasonryBlocksAcross, every other course set half a block over, each joint a little off
// its place and each block its own tone
func (s stone) masonry() look {
	length := min(max(s.w/visual.MasonryBlocksAcross, visual.MasonryMinLength), visual.MasonryMaxLength)
	u := s.x + s.y&1*length/2
	b := floorDiv(u, length)
	h := s.knot(channelCourse, b, s.y)
	if u < joint(b, length, h) {
		b--
		h = s.knot(channelCourse, b, s.y)
	} else if next := s.knot(channelCourse, b+1, s.y); u >= joint(b+1, length, next) {
		b, h = b+1, next
	}
	tone := share(h >> visual.StoneUnitBits)
	shade := (s.mottle - visual.StoneUnit/2) * visual.MasonryMottle / visual.StoneUnit
	s.bg = tint(color.LerpFixed(s.pal.Bg, s.pal.BgMottle, tone, visual.StoneUnitBits), shade)
	s.fg = s.pal.Fg
	s.r = visual.MasonryBed
	if u == joint(b, length, h) {
		s.r = visual.MasonryJoint
	}
	return s.look
}

// joint is where block b, hashed h, begins in its course, up to MasonryJointStray cells
// either side of its place
func joint(b, length int64, h uint64) int64 {
	return b*length + share(h)*(2*visual.MasonryJointStray+1)/visual.StoneUnit - visual.MasonryJointStray
}

// knot is a channel's hash at a lattice point: vmath.MixChain(seed, key) with the seed's
// round hoisted out of the cell, the key folding in the channel, the row and the column
func (s *stone) knot(channel uint64, kx, ky int64) uint64 {
	key := (channel*vmath.Golden64+uint64(ky))*vmath.Golden64 + uint64(kx)
	return vmath.Mix64((s.seed ^ key) + vmath.Golden64)
}

// share is a hash's low share of StoneUnit
func share(h uint64) int64 { return int64(h % visual.StoneUnit) }

// tint brightens c by a share of StoneUnit, or darkens it by a negative one
func tint(c color.RGB, by int64) color.RGB {
	f := visual.StoneUnit + by
	return color.RGB{R: clampByte(int64(c.R) * f), G: clampByte(int64(c.G) * f), B: clampByte(int64(c.B) * f)}
}

// clampByte brings a channel scaled by StoneUnit back to a byte
func clampByte(v int64) uint8 { return uint8(min(max(v/visual.StoneUnit, 0), math.MaxUint8)) }

// noise1 is a channel's value noise along the cell's row, knots 1<<w columns apart eased
// by smoothstep in fixed point: u²(3s−2u) over s³ at u of a spacing s
func (s *stone) noise1(channel uint64, w uint) int64 {
	k := s.x >> w
	u, span := s.x-k<<w, int64(1)<<w
	a, b := share(s.knot(channel, k, 0)), share(s.knot(channel, k+1, 0))
	return a + (b-a)*(u*u*(3*span-2*u))>>(3*w)
}

// noise2 is a channel's value noise at the cell, knots 1<<cw columns and 1<<rw rows apart
func (s *stone) noise2(channel uint64, cw, rw uint) int64 {
	kx, ky := s.x>>cw, s.y>>rw
	ux, uy := s.x-kx<<cw, s.y-ky<<rw
	wx, wy := int64(1)<<cw, int64(1)<<rw
	sx, sy := ux*ux*(3*wx-2*ux), uy*uy*(3*wy-2*uy)
	a, b := share(s.knot(channel, kx, ky)), share(s.knot(channel, kx+1, ky))
	c, d := share(s.knot(channel, kx, ky+1)), share(s.knot(channel, kx+1, ky+1))
	top, bottom := a<<(3*cw)+(b-a)*sx, c<<(3*cw)+(d-c)*sx
	return (top<<(3*rw) + (bottom-top)*sy) >> (3 * (cw + rw))
}

// floorDiv divides rounding down, so a pattern runs on unbroken across zero
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}
