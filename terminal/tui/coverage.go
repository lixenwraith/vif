package tui

// Coverage represents count/total for display in tree nodes
type Coverage struct {
	Count int
	Total int
}

func (c Coverage) IsAll() bool {
	return c.Count == c.Total && c.Total > 0
}

func (c Coverage) IsPartial() bool {
	return c.Count > 0 && c.Count < c.Total
}

func (c Coverage) IsNone() bool {
	return c.Count == 0
}

func (c Coverage) String() string {
	if c.Total == 0 {
		return ""
	}
	if c.IsAll() {
		return "[ALL]"
	}
	return "[" + intStr(c.Count) + "/" + intStr(c.Total) + "]"
}

// FormatCoverageSuffix returns a suffix string for TreeNode.Suffix
func FormatCoverageSuffix(count, total int) string {
	if total == 0 {
		return ""
	}
	if count == total {
		return " [ALL]"
	}
	return " [" + intStr(count) + "/" + intStr(total) + "]"
}

// intStr converts int to string without fmt dependency
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}