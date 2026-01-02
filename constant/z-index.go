package constant
// @lixen: #dev{base(core),feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

// Z-Index constants determine render priority and spatial query ordering
// Higher values render on top / take precedence in queries
const (
	ZIndexBackground = 0
	ZIndexGlyph      = 100
	ZIndexNugget     = 200
	ZIndexDecay      = 300
	ZIndexDrain      = 400
	ZIndexShield     = 500
	ZIndexCursor     = 1000
)