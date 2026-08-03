package status

import "strings"

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
