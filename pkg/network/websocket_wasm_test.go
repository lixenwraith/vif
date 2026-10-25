//go:build js && wasm

package network

import (
	"syscall/js"
	"testing"
	"time"
)

// TestAClosedBrowserSocketCallsNothingBack: a browser fires close after close()
// returns, so a listener released before it is taken off would be called released.
func TestAClosedBrowserSocketCallsNothingBack(t *testing.T) {
	global := js.Global()
	native, report := global.Get("WebSocket"), global.Get("console").Get("error")
	defer func() {
		global.Set("WebSocket", native)
		global.Get("console").Set("error", report)
	}()
	global.Call("eval", `
		globalThis.vifReported = [];
		console.error = (...args) => { vifReported.push(args.join(" ")); };
		globalThis.WebSocket = class {
			constructor() { this.on = {}; setTimeout(() => this.fire("open", {}), 0); }
			addEventListener(event, fn) { (this.on[event] ||= []).push(fn); }
			removeEventListener(event, fn) { this.on[event] = (this.on[event] || []).filter(f => f !== fn); }
			fire(event, e) { for (const fn of this.on[event] || []) fn(e); }
			close() { setTimeout(() => this.fire("close", {code: 1000}), 0); }
			send() {}
		};`)
	conn, err := dialWebSocket("wss://site.example/vif/ws/0123456789abcdef", time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	time.Sleep(20 * time.Millisecond) // the browser's turn, which fires the close
	if reported := global.Get("vifReported"); reported.Length() != 0 {
		t.Fatalf("closing reported %q", reported.Index(0).String())
	}
}
