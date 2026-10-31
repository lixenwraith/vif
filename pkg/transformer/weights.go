package transformer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
)

// The weight file: a magic and version, the Config, then every tensor by name
// with its shape and float32 data, little-endian, and the FNV-1a 64 of all of it.
const (
	fileMagic   = "VIFT"
	fileVersion = 1
	maxName     = 64
	maxTensor   = 1 << 28 // floats; a file past it is not one of ours
)

type tensor struct {
	name string
	dims []int
	data []float32
}

// tensors names every weight in a fixed order, the trainer's export matching it
func (m *Model) tensors() []tensor {
	lin := func(name string, l *Linear) []tensor {
		return []tensor{{name + ".weight", []int{l.Out, l.In}, l.W}, {name + ".bias", []int{l.Out}, l.B}}
	}
	norm := func(name string, ln *LayerNorm) []tensor {
		return []tensor{{name + ".weight", []int{len(ln.G)}, ln.G}, {name + ".bias", []int{len(ln.B)}, ln.B}}
	}
	ts := lin("in", &m.in)
	for i := range m.layers {
		l := &m.layers[i]
		p := fmt.Sprintf("layers.%d.", i)
		ts = append(ts, norm(p+"ln1", &l.ln1)...)
		ts = append(ts, lin(p+"q", &l.q)...)
		ts = append(ts, lin(p+"k", &l.k)...)
		ts = append(ts, lin(p+"v", &l.v)...)
		ts = append(ts, lin(p+"o", &l.o)...)
		ts = append(ts, norm(p+"ln2", &l.ln2)...)
		ts = append(ts, lin(p+"ff1", &l.ff1)...)
		ts = append(ts, lin(p+"ff2", &l.ff2)...)
	}
	ts = append(ts, norm("ln", &m.ln)...)
	return append(ts, lin("out", &m.out)...)
}

// Save writes the model as a weight file.
func (m *Model) Save(w io.Writer) error {
	h := fnv.New64a()
	bw := bufio.NewWriter(io.MultiWriter(w, h))
	le := binary.LittleEndian
	put := func(v any) {
		binary.Write(bw, le, v)
	}
	bw.WriteString(fileMagic)
	put(uint32(fileVersion))
	cfg := m.cfg
	put(uint32(cfg.Inputs))
	put(uint32(cfg.Dim))
	put(uint32(cfg.Heads))
	put(uint32(cfg.Layers))
	put(uint32(cfg.FFN))
	put(uint32(cfg.Window))
	put(uint32(len(cfg.Outputs)))
	for _, o := range cfg.Outputs {
		put(uint16(len(o.Name)))
		bw.WriteString(o.Name)
		put(uint32(o.Size))
	}
	ts := m.tensors()
	put(uint32(len(ts)))
	for _, t := range ts {
		put(uint16(len(t.name)))
		bw.WriteString(t.name)
		put(uint8(len(t.dims)))
		for _, d := range t.dims {
			put(uint32(d))
		}
		for _, f := range t.data {
			put(math.Float32bits(f))
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return binary.Write(w, le, h.Sum64())
}

// Load reads a weight file into a model of the Config it names.
func Load(r io.Reader) (*Model, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	if len(data) < 8 {
		return nil, errors.New("transformer: weight file cut short")
	}
	h := fnv.New64a()
	h.Write(data[:len(data)-8])
	if le.Uint64(data[len(data)-8:]) != h.Sum64() {
		return nil, errors.New("transformer: weight file checksum")
	}
	br := bytes.NewReader(data[:len(data)-8])
	var u32 uint32
	get := func(v any) error { return binary.Read(br, le, v) }
	magic := make([]byte, 4)
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != fileMagic {
		return nil, errors.New("transformer: not a weight file")
	}
	if err := get(&u32); err != nil || u32 != fileVersion {
		return nil, fmt.Errorf("transformer: weight file version %d, want %d", u32, fileVersion)
	}
	var cfg Config
	for _, p := range []*int{&cfg.Inputs, &cfg.Dim, &cfg.Heads, &cfg.Layers, &cfg.FFN, &cfg.Window} {
		if err := get(&u32); err != nil {
			return nil, err
		}
		*p = int(u32)
	}
	if err := get(&u32); err != nil || u32 > 64 {
		return nil, errors.New("transformer: output heads")
	}
	readName := func() (string, error) {
		var n uint16
		if err := get(&n); err != nil || n == 0 || n > maxName {
			return "", errors.New("transformer: a name's length")
		}
		b := make([]byte, n)
		_, err := io.ReadFull(br, b)
		return string(b), err
	}
	for range u32 {
		name, err := readName()
		if err != nil {
			return nil, err
		}
		var size uint32
		if err := get(&size); err != nil {
			return nil, err
		}
		cfg.Outputs = append(cfg.Outputs, Head{Name: name, Size: int(size)})
	}
	m, err := New(cfg)
	if err != nil {
		return nil, err
	}
	want := m.tensors()
	if err := get(&u32); err != nil || int(u32) != len(want) {
		return nil, fmt.Errorf("transformer: %d tensors, the shape wants %d", u32, len(want))
	}
	for _, t := range want {
		name, err := readName()
		if err != nil {
			return nil, err
		}
		var nd uint8
		if err := get(&nd); err != nil || int(nd) != len(t.dims) || name != t.name {
			return nil, fmt.Errorf("transformer: tensor %q of %d dims, want %q of %d", name, nd, t.name, len(t.dims))
		}
		n := 1
		for i := range t.dims {
			if err := get(&u32); err != nil || int(u32) != t.dims[i] {
				return nil, fmt.Errorf("transformer: tensor %q dim %d is %d, want %d", name, i, u32, t.dims[i])
			}
			n *= int(u32)
		}
		if n != len(t.data) || n > maxTensor {
			return nil, fmt.Errorf("transformer: tensor %q holds %d floats", name, n)
		}
		buf := make([]byte, 4*n)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, fmt.Errorf("transformer: tensor %q: %w", name, err)
		}
		for i := range t.data {
			t.data[i] = math.Float32frombits(le.Uint32(buf[4*i:]))
		}
	}
	if br.Len() != 0 {
		return nil, errors.New("transformer: weight file has bytes past its tensors")
	}
	return m, nil
}
