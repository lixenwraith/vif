package network

import (
	"bytes"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testBeacon = Beacon{
	Nonce: "00112233aabbccdd", Name: "friday", Scenario: "main", Players: 2, Ceiling: 8,
	Waiting: true, Protocol: 34, Build: "v1.2.3 (0a1b2c3d)",
	Doors: []BeaconDoor{{Scheme: "ws", Port: 7778}, {Scheme: "tcp", Port: 7777}},
}

// loopbackDiscovery is a discovery whose only network is this machine, sending to
// its port there and nowhere on the network the test runs in. With no port named
// it binds one the system gives it, which no other test can hold.
func loopbackDiscovery(t *testing.T, cfg DiscoveryConfig) *Discovery {
	t.Helper()
	cfg.Subnets = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")} }
	cfg.Rows, cfg.ProbeWindow, cfg.ProbesPerSource, cfg.Probes = 8, time.Minute, 4, 8
	d := NewDiscovery(cfg)
	d.limited = netip.Addr{}
	t.Cleanup(d.Close)
	return d
}

// visibleHost announces testBeacon, with a period no test outlasts: what reaches a
// looking run is its answer to a probe. It returns the port the host hears on.
func visibleHost(t *testing.T) int {
	t.Helper()
	listening := make(chan bool, 1)
	host := loopbackDiscovery(t, DiscoveryConfig{Interval: time.Hour, Expiry: time.Hour,
		Armed: func() bool { return true }, Beacon: func() *Beacon { return &testBeacon },
		Listening: func(on bool) { listening <- on }})
	host.Wake()
	if !<-listening {
		t.Fatal("a visible host opened no socket")
	}
	return host.conn.Load().LocalAddr().(*net.UDPAddr).Port
}

// FuzzDiscoveryDatagram: a beacon decodes only from the bytes its own encoding
// writes, so every field is within bounds and nothing else is read as one.
func FuzzDiscoveryDatagram(f *testing.F) {
	valid, err := EncodeBeacon(testBeacon)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(probeDatagram)
	f.Add(valid[:len(valid)-1])
	f.Add(append(bytes.Clone(valid), ' '))
	f.Add(bytes.Replace(valid, []byte(`"nonce"`), []byte(`"Nonce"`), 1))
	f.Add(bytes.Replace(valid, []byte(`"players":2`), []byte(`"players":2,"players":3`), 1))
	f.Add(bytes.Replace(valid, []byte(`main`), []byte(`main`), 1))
	f.Add(append(datagramHeader(datagramBeacon), `{"doors":null}`...))
	f.Fuzz(func(t *testing.T, p []byte) {
		b, err := DecodeBeacon(p)
		if err != nil {
			return
		}
		if again, err := EncodeBeacon(b); err != nil || !bytes.Equal(again, p) {
			t.Fatalf("decoded %q to %+v, which encodes to %q (%v)", p, b, again, err)
		}
	})
}

// TestTheLargestBeaconFitsADatagram: every field at its bound still encodes within
// DatagramSize, so a host's answer never outweighs the probe that asked for it.
func TestTheLargestBeaconFitsADatagram(t *testing.T) {
	b := Beacon{Nonce: strings.Repeat("f", beaconNonceLen), Name: strings.Repeat("n", beaconNameMax),
		Scenario: strings.Repeat("s", beaconTextMax), Build: strings.Repeat("b", beaconTextMax),
		Players: 65535, Ceiling: 65535, Protocol: 1<<32 - 1}
	for range beaconDoorsMax {
		b.Doors = append(b.Doors, BeaconDoor{Scheme: "wss", Port: 65535})
	}
	p, err := EncodeBeacon(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) > DatagramSize || len(probeDatagram) != DatagramSize {
		t.Fatalf("the largest beacon is %d bytes and a probe %d, want at most and exactly %d", len(p), len(probeDatagram), DatagramSize)
	}
}

// TestABeaconNamesTheDoorsOthersReach: a door bound to every interface or to an
// IPv4 address of this machine is named by its scheme and port, and one bound to
// the loopback or to an IPv6 address, where no beacon's source leads, is not.
func TestABeaconNamesTheDoorsOthersReach(t *testing.T) {
	got := BeaconDoors([]net.Addr{
		&net.TCPAddr{IP: net.IPv6unspecified, Port: 7777}, wsAddr("wss://0.0.0.0:7778"),
		&net.TCPAddr{IP: net.IPv4(192, 168, 1, 5), Port: 7779}, wsAddr("ws://127.0.0.1:7780"),
		&net.TCPAddr{IP: net.IPv6loopback, Port: 7781}, &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 7782},
	})
	want := []BeaconDoor{{"tcp", 7777}, {"wss", 7778}, {"tcp", 7779}}
	if !slices.Equal(got, want) {
		t.Fatalf("the doors named are %v, want %v", got, want)
	}
}

// TestAHostAnswersAFullSizeProbeAlone: a probe cut short is answered with nothing,
// and a whole one with the beacon, to its source.
func TestAHostAnswersAFullSizeProbeAlone(t *testing.T) {
	port := visibleHost(t)
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]byte, DatagramSize+1)
	if _, err := c.Write(probeDatagram[:discoveryHeader]); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, err := c.Read(buf); err == nil {
		t.Fatalf("a short probe was answered with %d bytes", n)
	}
	if _, err := c.Write(probeDatagram); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := DecodeBeacon(buf[:n]); err != nil || b.Scenario != testBeacon.Scenario {
		t.Fatalf("a probe was answered with %q (%v)", buf[:n], err)
	}
}

// TestALookingRunListsAVisibleHostAtOnce: a run that starts looking probes, and
// lists the host that answers at the address it answered from, with the link a
// join dials; the port the host holds does not stop a run on its machine looking.
func TestALookingRunListsAVisibleHostAtOnce(t *testing.T) {
	looker := loopbackDiscovery(t, DiscoveryConfig{Port: visibleHost(t), Interval: time.Hour, Expiry: time.Hour})
	deadline := time.Now().Add(5 * time.Second)
	var found []Found
	for len(found) == 0 && time.Now().Before(deadline) {
		found = looker.Look()
		time.Sleep(5 * time.Millisecond)
	}
	if len(found) != 1 {
		t.Fatalf("a looking run listed %d sessions, want the host's", len(found))
	}
	if got, want := found[0].Target(), "vif://127.0.0.1:7777/friday"; got != want {
		t.Errorf("the session is joined at %s, want %s", got, want)
	}
}

// TestASessionIsListedOnceWhateverAddressesItAnswersFrom: a host on several
// networks answers from each, and the list holds its session once, at the lowest.
func TestASessionIsListedOnceWhateverAddressesItAnswersFrom(t *testing.T) {
	d := loopbackDiscovery(t, DiscoveryConfig{Interval: time.Hour, Expiry: time.Hour})
	other := testBeacon
	other.Nonce = "ffeeddccbbaa0099"
	for _, f := range []Found{
		{testBeacon, netip.MustParseAddr("192.168.122.1")}, {testBeacon, netip.MustParseAddr("192.168.1.72")},
		{other, netip.MustParseAddr("192.168.1.80")},
	} {
		d.note(f)
	}
	found := d.Look()
	if len(found) != 2 || found[0].Address() != "192.168.1.72:7777" || found[1].Address() != "192.168.1.80:7777" {
		t.Fatalf("two sessions, one answering from two addresses, are listed as %+v", found)
	}
}

// TestARunThatNeitherAnnouncesNorLooksHoldsNoSocket: the socket opens when a run
// starts looking and closes once nobody has asked for the list for Expiry.
func TestARunThatNeitherAnnouncesNorLooksHoldsNoSocket(t *testing.T) {
	var open atomic.Int32
	d := loopbackDiscovery(t, DiscoveryConfig{Interval: 5 * time.Millisecond, Expiry: 40 * time.Millisecond,
		Listening: func(on bool) {
			if on {
				open.Add(1)
			} else {
				open.Add(-1)
			}
		}})
	wait := func(want int32) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); open.Load() != want; time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%d sockets open, want %d", open.Load(), want)
			}
		}
	}
	d.Look()
	wait(1)
	wait(0)
	d.Look()
	wait(1)
}

// TestAHostAnswersItsOwnNetworksWithinABudget: a probe is answered from a private,
// link-local or loopback source or one inside this machine's networks, a source's
// share and the whole budget each run out, and a new window starts both over.
func TestAHostAnswersItsOwnNetworksWithinABudget(t *testing.T) {
	local := []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")}
	for addr, want := range map[string]bool{
		"192.168.1.9": true, "10.1.2.3": true, "169.254.7.7": true, "127.0.0.1": true,
		"203.0.113.200": true, "203.0.114.1": false, "8.8.8.8": false,
	} {
		if got := onLink(netip.MustParseAddr(addr), local); got != want {
			t.Errorf("a probe from %s is answered: %v, want %v", addr, got, want)
		}
	}
	cfg := &DiscoveryConfig{ProbeWindow: time.Second, ProbesPerSource: 2, Probes: 3}
	a, b, c := netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("10.0.0.3")
	var budget probeBudget
	now := time.Unix(1000, 0)
	for i, step := range []struct {
		from netip.Addr
		at   time.Duration
		want bool
	}{
		{a, 0, true}, {a, 0, true}, {a, 0, false}, {b, 0, true}, {c, 0, false},
		{a, time.Second, true}, {c, time.Second, true},
	} {
		if got := budget.take(step.from, now.Add(step.at), cfg); got != step.want {
			t.Errorf("step %d: a probe from %s is answered: %v, want %v", i, step.from, got, step.want)
		}
	}
}
