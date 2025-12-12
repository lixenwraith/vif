// @lixen: #focus{lifecycle[cull,marker,death]}
// @lixen: #interact{state[death]}
package components

// MarkedForDeath tags an entity to be destroyed by CullSystem safely after game logic
type MarkedForDeathComponent struct{}