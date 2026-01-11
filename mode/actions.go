package mode

// executeRepeatFind repeats the last find/till command
func (r *Router) executeRepeatFind(reverse bool) {
	if r.lastFindType == 0 {
		return
	}

	pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
	if !ok {
		return
	}

	originalChar := r.lastFindChar
	originalType := r.lastFindType
	originalForward := r.lastFindForward

	var charMotion CharMotionFunc

	// Determine motion based on direction and reversal
	if reverse {
		switch r.lastFindType {
		case 'f':
			charMotion = MotionFindBack
		case 'F':
			charMotion = MotionFindForward
		case 't':
			charMotion = MotionTillBack
		case 'T':
			charMotion = MotionTillForward
		}
	} else {
		switch r.lastFindType {
		case 'f':
			charMotion = MotionFindForward
		case 'F':
			charMotion = MotionFindBack
		case 't':
			charMotion = MotionTillForward
		case 'T':
			charMotion = MotionTillBack
		}
	}

	result := charMotion(r.ctx, pos.X, pos.Y, 1, r.lastFindChar)
	OpMove(r.ctx, result)

	// Restore original state because OpMove/CharMotion logic might update it to the 'reversed' type
	r.lastFindChar = originalChar
	r.lastFindType = originalType
	r.lastFindForward = originalForward
}