package logfile

import (
	"bytes"
	"strconv"
)

// Column identifies a list column for focus, search scope and sorting.
type Column uint8

const (
	ColAll Column = iota
	ColTime
	ColTick
	ColSub
	ColMsg
	ColFields
	ColCount
)

var columnName = [ColCount]string{"all", "time", "tick", "sub", "msg", "fields"}

func (c Column) String() string {
	if c < ColCount {
		return columnName[c]
	}
	return "?"
}

// Next returns the column d steps away, wrapping.
func (c Column) Next(d int) Column {
	return Column(((int(c)+d)%int(ColCount) + int(ColCount)) % int(ColCount))
}

// Field is one member of the fields object; Val is the raw JSON token.
type Field struct {
	Key  string
	Val  []byte
	Kind byte
}

// Record is the parsed form of a line. It is the only type that knows the log
// schema: a new sub, a renamed key or a second format touches this file alone.
type Record struct {
	Meta   Meta
	Raw    []byte
	Time   string
	Level  string
	Sub    string
	Trace  string
	Fields []Field
	Bad    bool

	msgIdx int    // index into Fields of the discriminator, -1 if none
	buf    []byte // column rendering scratch, reused across calls
}

// msgKeys are the discriminator keys in precedence order. Most records use
// fields.msg; the logger self-report uses fields.type.
var msgKeys = [...]string{"msg", "type"}

// Parse fills r from line. Slices alias line and are reused across calls, so
// copy anything retained past the next Parse.
func (r *Record) Parse(m Meta, line []byte) {
	r.Meta, r.Raw = m, line
	r.Time, r.Level, r.Sub, r.Trace = "", "", "", ""
	r.Fields = r.Fields[:0]
	r.msgIdx = -1
	r.Bad = m.Flags&FlagMalformed != 0 || len(line) == 0
	if r.Bad {
		return
	}

	eachField(line, skipSpace(line, 0), func(k, v []byte, kind byte) bool {
		switch string(k) {
		case "time":
			r.Time = string(strTok(v))
		case "level":
			r.Level = string(strTok(v))
		case "sub":
			r.Sub = string(strTok(v))
		case "trace":
			r.Trace = unquote(v)
		case "fields":
			if kind == KObj {
				eachField(v, 0, func(fk, fv []byte, fkind byte) bool {
					r.Fields = append(r.Fields, Field{Key: string(fk), Val: fv, Kind: fkind})
					return true
				})
			}
		}
		return true
	})

	// Resolve the discriminator once: input records carry both msg and type,
	// and only the one actually used may be dropped from the fields text.
	for _, k := range msgKeys {
		for i := range r.Fields {
			if r.Fields[i].Key == k && r.Fields[i].Kind == KStr {
				r.msgIdx = i
				break
			}
		}
		if r.msgIdx >= 0 {
			break
		}
	}
}

// Get returns the named field.
func (r *Record) Get(key string) (Field, bool) {
	for _, f := range r.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Msg returns the record's discriminator.
func (r *Record) Msg() string {
	if r.msgIdx < 0 {
		return ""
	}
	return unquote(r.Fields[r.msgIdx].Val)
}

// FollowValue returns the first string field other than the discriminator: the
// value distinguishing records that share a (sub, msg) pair — ev for event
// dispatch, service for service records. Empty when every field is numeric.
func (r *Record) FollowValue() string {
	for i, f := range r.Fields {
		if i == r.msgIdx || f.Kind != KStr {
			continue
		}
		return unquote(f.Val)
	}
	return ""
}

const fieldsTextCap = 512

// FieldsText is the fields column exactly as the record list renders it.
func (r *Record) FieldsText() string {
	r.buf = r.appendFields(r.buf[:0])
	return string(r.buf)
}

// ColumnBytes renders one column's searchable text into a reused buffer, so
// search matches what is on screen rather than the raw JSON.
func (r *Record) ColumnBytes(c Column) []byte {
	r.buf = r.buf[:0]
	switch c {
	case ColTime:
		r.buf = append(r.buf, StampText(r.Meta)...)
	case ColTick:
		r.buf = strconv.AppendUint(r.buf, uint64(r.Meta.Tick), 10)
	case ColSub:
		r.buf = append(r.buf, r.Sub...)
	case ColMsg:
		r.buf = r.appendMsg(r.buf)
	case ColFields:
		r.buf = r.appendFields(r.buf)
	default:
		r.buf = append(r.buf, StampText(r.Meta)...)
		r.buf = append(r.buf, ' ')
		r.buf = append(r.buf, r.Sub...)
		r.buf = append(r.buf, ' ')
		r.buf = r.appendMsg(r.buf)
		r.buf = append(r.buf, ' ')
		r.buf = r.appendFields(r.buf)
	}
	return r.buf
}

func (r *Record) appendMsg(dst []byte) []byte {
	if r.msgIdx < 0 {
		return dst
	}
	return appendUnquoted(dst, r.Fields[r.msgIdx].Val)
}

func (r *Record) appendFields(dst []byte) []byte {
	start := len(dst)
	for i, f := range r.Fields {
		if i == r.msgIdx {
			continue
		}
		if len(dst) > start {
			dst = append(dst, ' ')
		}
		dst = append(dst, f.Key...)
		dst = append(dst, '=')
		dst = r.appendDisplay(dst, f)
		if len(dst)-start > fieldsTextCap {
			break
		}
	}
	return dst
}

// Display renders a field value: durations via the unit table, long float
// tails trimmed, everything else verbatim.
func (r *Record) Display(f Field) string {
	return string(r.appendDisplay(nil, f))
}

func (r *Record) appendDisplay(dst []byte, f Field) []byte {
	switch f.Kind {
	case KStr:
		return appendUnquoted(dst, f.Val)
	case KNum:
		if u := DurationUnit(f.Key); u != DurNone {
			if v, ok := parseInt64(f.Val); ok {
				return append(dst, FormatDuration(v, u)...)
			}
		}
		return append(dst, trimFloat(f.Val)...)
	}
	return append(dst, f.Val...)
}

func appendUnquoted(dst, tok []byte) []byte {
	in := strTok(tok)
	if bytes.IndexByte(in, '\\') < 0 {
		return append(dst, in...)
	}
	return append(dst, unquote(tok)...)
}

// trimFloat shortens 17-digit float tails to 6 significant digits.
func trimFloat(tok []byte) []byte {
	dot := bytes.IndexByte(tok, '.')
	if dot < 0 || len(tok)-dot <= 7 {
		return tok
	}
	if v, err := strconv.ParseFloat(string(tok), 64); err == nil {
		return strconv.AppendFloat(nil, v, 'g', 6, 64)
	}
	return tok
}
