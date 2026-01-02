package engine
// @lixen: #dev{feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

// Entity is defined in core package to avoid cyclic dependency

// Components are handled in Store

// System is an interface that all systems must implement
type System interface {
	Init()
	Update()
	Priority() int // Lower values run first
}