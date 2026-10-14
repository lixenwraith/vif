// Package network is the session transport: framed messages over TCP or WebSocket,
// deflated each way per link once its handshake is done, and an in-process
// mesh for tests; the join and peer-link handshakes and per-address admission
// budget; chunked snapshot framing; the link probe a port answers itself; and the
// authority term, succession rule and chain. See doc/services-and-networking.md.
package network
