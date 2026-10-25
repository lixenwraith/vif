// Package network is the link layer: framed messages over TCP or WebSocket, deflated
// per link once a handshake opens the stream, or over an in-process mesh with shaped
// links; the accept and dial transport with its handshake budget; the port's
// tick-drained inbound queue, bundled sends and link probe; chunked bodies; crossing
// fences. It knows no game: the session layer names the kinds and the handshakes.
package network
