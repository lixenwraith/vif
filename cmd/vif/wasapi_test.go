//go:build windows && !vif_headless && !vif_nogui && !vif_noaudio

package main

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// fakeDevice stands in for the two interfaces the render thread calls, as tables
// of callbacks, so the bridge is tested where no device is: it holds what it is
// given and, while it takes, plays a period of it every 10 ms
type fakeDevice struct {
	mu     sync.Mutex
	queued uint32 // frames held, as GetCurrentPadding answers
	takes  bool
	fails  bool
	buffer [8192]byte
	got    []byte
}

var fake *fakeDevice

// A callback is never freed, so the tables are made once for every test
var fakeClient, fakeRender = func() (*comObject, *comObject) {
	var client, render [15]uintptr
	none := windows.NewCallback(func(*comObject) uintptr { return 0 })
	client[comRelease], render[comRelease], client[clientStop] = none, none, none
	client[clientGetCurrentPadding] = windows.NewCallback(func(_ *comObject, frames *uint32) uintptr {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.fails {
			return 0x88890004 // AUDCLNT_E_DEVICE_INVALIDATED
		}
		*frames = fake.queued
		return 0
	})
	render[renderGetBuffer] = windows.NewCallback(func(_ *comObject, _ uintptr, data **byte) uintptr {
		*data = &fake.buffer[0]
		return 0
	})
	render[renderReleaseBuffer] = windows.NewCallback(func(_ *comObject, frames, _ uintptr) uintptr {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.got = append(fake.got, fake.buffer[:frames*4]...)
		fake.queued += uint32(frames)
		return 0
	})
	return &comObject{table: &client}, &comObject{table: &render}
}()

// fakeSpeaker is a sink on a fresh stand-in device, its render thread playing
// then shutting as run does; done closes when it has
func fakeSpeaker(t *testing.T) *wasapiSpeaker {
	device := &fakeDevice{takes: true}
	fake = device
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s := &wasapiSpeaker{event: event, done: make(chan struct{}), taken: make(chan struct{}, 1), stall: time.NewTimer(wasapiStall),
		playing: true, client: fakeClient, render: fakeRender, period: 441, target: 882, starved: true, opened: now, moved: now}
	s.stall.Stop()
	go func() {
		runtime.LockOSThread()
		s.play()
		s.shut()
		close(s.done)
	}()
	go func() {
		for tick := time.NewTicker(10 * time.Millisecond); ; {
			select {
			case <-s.done:
				tick.Stop()
				return
			case <-tick.C:
			}
			device.mu.Lock()
			if device.takes {
				device.queued -= min(device.queued, 441)
			}
			device.mu.Unlock()
			windows.SetEvent(event)
		}
	}()
	return s
}

// The mixer's writes are handed to the render thread a slice at a time, whatever
// the mixer's period is against the device's: nothing is lost, doubled or reordered
func TestEveryFrameWrittenReachesTheDeviceOnceAndInOrder(t *testing.T) {
	for _, frames := range []int{441, 882, 2205, 8820} {
		s := fakeSpeaker(t)
		var want []byte
		for n := uint32(0); len(want) < 4*44100/2; {
			b := make([]byte, 4*frames)
			for i := 0; i < len(b); i += 4 {
				binary.LittleEndian.PutUint32(b[i:], n)
				n++
			}
			want = append(want, b...)
			if _, err := s.Write(b); err != nil {
				t.Fatalf("%d frames a write: %v", frames, err)
			}
		}
		s.Close()
		if !bytes.Equal(fake.got, want) {
			t.Errorf("%d frames a write: the device was given %d bytes that are not the %d written", frames, len(fake.got), len(want))
		}
	}
}

// A write error silences the engine for the run, so a stream that fails or stops
// taking is given up by the render thread, which reopens, and the mixer hears nothing
func TestAStreamThatFailsOrStallsIsGivenUpWithoutAnErrorToTheMixer(t *testing.T) {
	for name, breakIt := range map[string]func(){"fails": func() { fake.fails = true }, "takes nothing": func() { fake.takes = false }} {
		s := fakeSpeaker(t)
		block := make([]byte, 4*882)
		for range 5 {
			s.Write(block)
		}
		fake.mu.Lock()
		breakIt()
		fake.mu.Unlock()
		for gaveUp := false; !gaveUp; {
			if _, err := s.Write(block); err != nil {
				t.Fatalf("a device that %s: the write returned %v", name, err)
			}
			select {
			case <-s.done:
				gaveUp = true
			case <-time.After(10 * time.Millisecond):
			}
		}
		s.Close()
	}
}
