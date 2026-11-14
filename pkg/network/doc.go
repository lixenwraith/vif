// Package network is the link layer: framed messages over TCP or WebSocket, deflated
// per link once a handshake opens the stream, or over an in-process mesh of shaped
// links; the accept and dial transport and its handshake budget; the port's drained
// queue, bundled sends and link probe; chunked bodies; crossing fences; the one-way
// feed copied to subscribers. It knows no game: the session layer names the kinds.
package network
