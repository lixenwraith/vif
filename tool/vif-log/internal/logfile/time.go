package logfile

// parseRFC3339Nano parses an RFC3339 stamp with optional fractional seconds
// into unix nanoseconds. Hand-rolled to keep the index pass allocation-free.
func parseRFC3339Nano(b []byte) (int64, bool) {
	if len(b) < 19 {
		return 0, false
	}
	if b[4] != '-' || b[7] != '-' || (b[10] != 'T' && b[10] != 't') || b[13] != ':' || b[16] != ':' {
		return 0, false
	}
	y, ok1 := numField(b[0:4])
	mo, ok2 := numField(b[5:7])
	d, ok3 := numField(b[8:10])
	h, ok4 := numField(b[11:13])
	mi, ok5 := numField(b[14:16])
	s, ok6 := numField(b[17:19])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return 0, false
	}

	i := 19
	var frac int64
	if i < len(b) && b[i] == '.' {
		scale := int64(100000000)
		for i++; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
			if scale > 0 {
				frac += int64(b[i]-'0') * scale
				scale /= 10
			}
		}
	}

	var offSec int64
	if i < len(b) {
		switch b[i] {
		case 'Z', 'z':
		case '+', '-':
			if i+6 > len(b) || b[i+3] != ':' {
				return 0, false
			}
			oh, okh := numField(b[i+1 : i+3])
			om, okm := numField(b[i+4 : i+6])
			if !okh || !okm {
				return 0, false
			}
			offSec = int64(oh)*3600 + int64(om)*60
			if b[i] == '-' {
				offSec = -offSec
			}
		}
	}

	sec := daysFromCivil(y, mo, d)*86400 + int64(h)*3600 + int64(mi)*60 + int64(s) - offSec
	return sec*1e9 + frac, true
}

func numField(b []byte) (int, bool) {
	v := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

// daysFromCivil returns days since 1970-01-01 (Hinnant's civil-date algorithm).
func daysFromCivil(y, m, d int) int64 {
	if m <= 2 {
		y--
	}
	era := y
	if era < 0 {
		era -= 399
	}
	era /= 400
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return int64(era)*146097 + int64(doe) - 719468
}
