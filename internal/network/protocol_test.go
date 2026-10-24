package network

import (
	"bytes"
	"errors"
	"io"
	"net"
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

// teeConn records what its reader side took off the wire.
type teeConn struct {
	net.Conn
	rec *bytes.Buffer
}

func (c teeConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.rec.Write(p[:n])
	return n, err
}

// streamPair is an opened link: frames written on w and flushed are read on r, the
// wire bytes r saw recorded in rec.
func streamPair(t *testing.T) (w, r *flateStream, rec *bytes.Buffer) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	rec = &bytes.Buffer{}
	return openFlateStream(a, 0, 0), openFlateStream(teeConn{b, rec}, 0, 0), rec
}

// readFrames reads n frames off r, in order.
func readFrames(t *testing.T, r *flateStream, n int) []*Message {
	t.Helper()
	var header [HeaderSize]byte
	out := make([]*Message, 0, n)
	for range n {
		msg, err := r.readFrame(MaxPayloadSize, header[:])
		if err != nil {
			t.Fatalf("frame %d: %v", len(out), err)
		}
		out = append(out, msg)
	}
	return out
}

// TestADeflatedBodyPassesTheStreamRaw: a frame whose body the snapshot codec
// deflated is not deflated again by the link, and keeps its place among the frames
// the link does deflate, which a run delivers whole as soon as it is flushed.
func TestADeflatedBodyPassesTheStreamRaw(t *testing.T) {
	w, r, rec := streamPair(t)
	body := bytes.Repeat([]byte("VIFS a body the codec already deflated "), 100)
	bursts := [][]*Message{
		{
			{Type: MsgEvent, Payload: []byte("an epoch")},
			{Type: MsgStateSync, Payload: []byte("a sync behind it")},
			{Type: MsgStateDigest, Payload: []byte("a digest behind that")},
		},
		{
			{Type: MsgEvent, Payload: []byte("the next epoch")},
			{Type: MsgStateSync, Payload: []byte("its sync")},
			{Type: MsgStateCorrection, Payload: body},
			{Type: MsgStateShard, Payload: body[:50]},
			{Type: MsgEvent, Payload: []byte("the epoch after the body")},
		},
	}
	read := make(chan struct{})
	go func() {
		var header [HeaderSize]byte
		for _, burst := range bursts {
			for _, f := range burst {
				if err := w.writeFrame(f, header[:]); err != nil {
					return
				}
			}
			if err := w.Flush(); err != nil {
				return
			}
			<-read // the next burst is written only once this one was read whole
		}
	}()
	for _, burst := range bursts {
		for i, got := range readFrames(t, r, len(burst)) {
			if got.Type != burst[i].Type || !bytes.Equal(got.Payload, burst[i].Payload) {
				t.Fatalf("frame %d read as %#x %q, want %#x", i, got.Type, got.Payload, burst[i].Type)
			}
		}
		read <- struct{}{}
	}
	if !bytes.Contains(rec.Bytes(), body) {
		t.Fatal("the body was deflated by the link")
	}
	if bytes.Contains(rec.Bytes(), bursts[0][0].Payload) {
		t.Fatal("the epoch travelled plain")
	}
}

// TestARawFrameDueOnTheWindowBoundaryStaysInTheRun: the inflater delivers a run that
// ends exactly on its window before it reads the run's flush marker, so a raw frame
// sent next would be read as deflate; the writer deflates that frame instead.
func TestARawFrameDueOnTheWindowBoundaryStaysInTheRun(t *testing.T) {
	w, r, _ := streamPair(t)
	frames := []*Message{
		{Type: MsgEvent, Payload: bytes.Repeat([]byte("v"), flateWindow-HeaderSize)},
		{Type: MsgStateCorrection, Payload: []byte("a body due exactly on the boundary")},
		{Type: MsgEvent, Payload: []byte("the epoch after")},
	}
	go func() {
		var header [HeaderSize]byte
		for _, f := range frames {
			if w.writeFrame(f, header[:]) != nil || w.Flush() != nil {
				return
			}
		}
	}()
	for i, got := range readFrames(t, r, len(frames)) {
		if got.Type != frames[i].Type || !bytes.Equal(got.Payload, frames[i].Payload) {
			t.Fatalf("frame %d read as %#x, want %#x", i, got.Type, frames[i].Type)
		}
	}
}
