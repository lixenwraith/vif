package network

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type shortWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	return w.Buffer.Write(p)
}

type shortReader struct {
	r     io.Reader
	limit int
}

func (r shortReader) Read(p []byte) (int, error) {
	if len(p) > r.limit {
		p = p[:r.limit]
	}
	return r.r.Read(p)
}

func TestFrameRoundTripSurvivesShortStreamIO(t *testing.T) {
	w := &shortWriter{limit: 2}
	want := &Message{Type: MsgEvent, Flags: FlagNeedAck, Seq: 41, Ack: 17, Payload: []byte("one complete frame")}
	if err := want.Encode(w); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(shortReader{r: bytes.NewReader(w.Bytes()), limit: 1})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != want.Type || got.Flags != want.Flags || got.Seq != want.Seq || got.Ack != want.Ack || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestDecodeRejectsPartialPayload(t *testing.T) {
	var frame bytes.Buffer
	if err := (&Message{Type: MsgEvent, Payload: []byte("whole")}).Encode(&frame); err != nil {
		t.Fatal(err)
	}
	b := frame.Bytes()
	got, err := Decode(bytes.NewReader(b[:len(b)-1]))
	if got != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode(partial) = (%#v, %v), want nil, unexpected EOF", got, err)
	}
}
