package status

import (
	"strconv"
	"strings"
)

// GroupMisc receives metrics registered without a "group.name" key
const GroupMisc = "misc"

// SplitKey splits "group.name" into its parts; keys without a separator
// fall into GroupMisc. Sole owner of the grouping convention.
func SplitKey(key string) (group, name string) {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return GroupMisc, key
}

// PlayerKey builds a per-slot metric key: PlayerKey(2, "energy.current") = "player.2.energy.current"
// Called at construction only; the resulting cell pointer is cached by the caller.
func PlayerKey(slot int, suffix string) string {
	return "player." + strconv.Itoa(slot) + "." + suffix
}
