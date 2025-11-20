# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game written in Go using an ECS (Entity Component System) architecture. The game features spatial indexing, temporal synchronization, and multiple gameplay systems including spawning, scoring, decay, and cleaner mechanics.

## CRITICAL DIRECTIVES

### 1. CODE STRUCTURE ADHERENCE
- **NEVER** deviate from existing project structure without explicit request
- **ALWAYS** follow the established patterns in the codebase:
  - ECS components in `engine/components.go`
  - Systems in `systems/` directory with `Priority()` method
  - Documentation in `doc/` directory (architecture.md, game.md, etc.)
  - Assets in `assets/` directory
  - Constants in `constants/game_constants.go`

### 2. CONSTANT MANAGEMENT
**MANDATORY**: Never use hard-coded values. All constants must be:
- Defined in `constants/game_constants.go` or relevant system constants file
- Named descriptively (e.g., `DecayIntervalMs`, not `50`)
- Referenced consistently throughout codebase
- Used in tests via constant references, not literals
```go
// ❌ WRONG
if elapsed >= 50 * time.Millisecond
TestDecayAfter50Ms() // Hard-coded in function name

// ✅ CORRECT
if elapsed >= DecayInterval
TestDecayAfterInterval() // Uses constant reference
```

### 3. SINGLE SOURCE OF TRUTH
**RULE**: One reference for each concept. No duplicate definitions.
- Check for existing constants/types before creating new ones
- If found duplicate (e.g., two sets of alphanumeric characters), consolidate immediately
- Use type aliases or references, never duplicate data structures
```go
// ❌ WRONG - Multiple definitions
var AlphaNumRunes = []rune{'a','b','c'...}
var AlphaNumString = "abc..."

// ✅ CORRECT - Single source
var AlphaNumRunes = []rune{'a','b','c'...}
func AlphaNumString() string { return string(AlphaNumRunes) }
```

### 4. COMMENT SYNCHRONIZATION
**REQUIREMENT**: Update ALL comments when changing code:
- Search entire codebase for old terminology when renaming
- Update function/method comments when behavior changes
- Keep test descriptions aligned with test implementation
- Use semantic search to find all references
```go
// When renaming "Character" to "Sequence":
// 1. Update code
// 2. Search for "character" in all .go files
// 3. Update every comment, including test descriptions
// 4. Update documentation in doc/
```

### 5. ROOT CAUSE ANALYSIS
**APPROACH**: Always identify and fix root causes:
1. When bug found, trace to origin system
2. Identify systemic pattern causing issue
3. Fix at architectural level, not symptom level
4. Add tests for root cause, not just symptom

Example from codebase:
```go
// ❌ SYMPTOM FIX: Add collision check in each system
// ✅ ROOT FIX: Implement spatial transaction system for atomic operations
```

### 6. TEST MANAGEMENT
**RULES**:
- **UPDATE** existing tests when behavior changes - DO NOT add duplicates
- **REMOVE** obsolete tests when functionality removed
- **CONSOLIDATE** similar tests into parameterized table tests
- Test files should be 20-30% of codebase size, not 500%
```go
// ❌ WRONG - Adding new test for changed behavior
func TestOldBehavior(t *testing.T) { ... }
func TestNewBehavior(t *testing.T) { ... } // Added instead of updating

// ✅ CORRECT - Update existing test
func TestBehavior(t *testing.T) { 
    // Updated to reflect new requirements
}
```

### 7. DOCUMENTATION UPDATES
**MANDATORY** after every architectural change:
1. Update relevant files in `doc/` directory:
   - `architecture.md` - System design changes
   - `game.md` - Gameplay mechanic changes
   - `README.md` - Setup/usage changes
2. Use consistent terminology across all docs
3. Include code examples where relevant
4. Update immediately, not as afterthought

## IMPLEMENTATION PATTERNS

### Spatial Operations
Always use transaction pattern for spatial index updates:
```go
tx := world.BeginSpatialTransaction()
result := tx.Move(entity, oldX, oldY, newX, newY)
if result.HasCollision {
    // Handle collision
}
tx.Commit()
```

### System Creation
```go
type NewSystem struct {
    world *engine.World
    // fields...
}

func (s *NewSystem) Priority() int { return PRIORITY_CONSTANT }
func (s *NewSystem) Update(ctx *engine.GameContext, delta time.Duration) error {
    // Implementation
}
```

### Component Access
```go
// Always check for component existence
if world.HasComponent(entity, reflect.TypeOf((*SequenceComponent)(nil))) {
    seq := world.GetComponent(entity, reflect.TypeOf((*SequenceComponent)(nil))).(*SequenceComponent)
    // Use component
}
```

### Atomic Counter Updates
```go
// For thread-safe color counting
atomic.AddInt64(&blueNormalCount, 1)
if atomic.LoadInt64(&blueNormalCount) == 0 {
    // Color available for spawning
}
```

## CURRENT ISSUES TO ADDRESS

### Spatial Index Race Condition
- **Problem**: `UpdateSpatialIndex()` overwrites without checking
- **Solution**: Implement `engine/spatial_transactions.go`
- **Pattern**: All spatial updates must use transaction system

### Temporal Desynchronization
- **Problem**: Different time domains (50ms clock, 16ms render)
- **Solution**: Implement interpolation for smooth movement
- **Pattern**: Use `InterpolatedPosition` for cross-domain entities

### Test Time-Step Issues
- **Problem**: Large time steps cause collision "tunneling"
- **Solution**: Use smaller time steps (10ms) in tests
- **Pattern**: `const testTimeStep = 10 * time.Millisecond`

## FILE ORGANIZATION
```
vi-fighter/
├── engine/              # Core ECS engine
│   ├── components.go    # Component definitions
│   ├── ecs.go          # World and entity management
│   └── spatial_transactions.go  # NEW: Atomic spatial operations
├── systems/            # Game systems
│   ├── *_system.go    # Individual systems
│   └── *_test.go      # System tests (keep minimal)
├── constants/          # All game constants
│   └── game_constants.go
├── content/           # Content management
├── assets/            # Game assets (source code files)
├── doc/              # Documentation (ALWAYS UPDATE)
│   ├── architecture.md
│   ├── game.md
│   └── analysis/     # Technical analyses
└── main.go
```

## VERIFICATION CHECKLIST

Before committing any change:
- [ ] No hard-coded values in code
- [ ] All comments updated for terminology changes
- [ ] Tests updated, not duplicated
- [ ] Documentation in doc/ updated
- [ ] Root cause addressed, not just symptom
- [ ] Single source of truth maintained
- [ ] Follows existing project structure

## COMMON MISTAKES TO AVOID

1. **Creating new constant sets instead of reusing**
2. **Adding "fix" commits without understanding root cause**
3. **Leaving outdated comments after refactoring**
4. **Creating test_new.go instead of updating test.go**
5. **Implementing workarounds instead of fixing architecture**
6. **Forgetting to update doc/ after changes**
7. **Using magic numbers in any context**

## REMEMBER
- The codebase is the source of truth for patterns
- When in doubt, search for existing examples
- Update everything affected by a change in one commit
- Quality over quantity - fewer, better tests
- Documentation is not optional
