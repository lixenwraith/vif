package audio

// EuclidMask returns the Bresenham onset mask for E(k,n), rotated by rot steps
// Equivalent onset distribution to Bjorklund; bit i = onset at step i; n <= 64
func EuclidMask(k, n, rot int) uint64 {
	if n <= 0 || n > 64 || k <= 0 {
		return 0
	}
	full := ^uint64(0)
	if n < 64 {
		full = (uint64(1) << uint(n)) - 1
	}
	if k >= n {
		return full
	}
	var m uint64
	for i := range n {
		if (i*k)%n < k {
			m |= 1 << uint(i)
		}
	}
	rot = ((rot % n) + n) % n
	if rot != 0 {
		m = ((m >> uint(rot)) | (m << uint(n-rot))) & full
	}
	return m
}
