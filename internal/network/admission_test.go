package network

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// TestAProofIsGoodAtOneDoorForOneNonce: a key's proof verifies under the nonce, the
// session and the binding it was made for and under no other, and only as that key's.
func TestAProofIsGoodAtOneDoorForOneNonce(t *testing.T) {
	k, other := NewKey(), NewKey()
	nonce, binding := NewNonce(), ChannelBinding([]byte("a leaf certificate"))
	proof := k.SignProof(nonce, "0123456789abcdef", binding)
	if !VerifyProof(k.Public(), proof, nonce, "0123456789abcdef", binding) {
		t.Fatal("a proof is refused at the door it was made for")
	}
	for name, ok := range map[string]bool{
		"another nonce":     VerifyProof(k.Public(), proof, NewNonce(), "0123456789abcdef", binding),
		"another session":   VerifyProof(k.Public(), proof, nonce, "fedcba9876543210", binding),
		"an unbound door":   VerifyProof(k.Public(), proof, nonce, "0123456789abcdef", nil),
		"another key":       VerifyProof(other.Public(), proof, nonce, "0123456789abcdef", binding),
		"no key":            VerifyProof(nil, proof, nonce, "0123456789abcdef", binding),
		"a truncated proof": VerifyProof(k.Public(), proof[:len(proof)-1], nonce, "0123456789abcdef", binding),
	} {
		if ok {
			t.Errorf("%s verifies", name)
		}
	}
	if len(nonce) < 20 || nonce == NewNonce() {
		t.Errorf("a nonce is %q", nonce)
	}
}

// TestAKeyIsKeptAsItsSeed: a key stored and read back signs as the same key, and
// bytes of another length or an empty key are refused rather than read as one.
func TestAKeyIsKeptAsItsSeed(t *testing.T) {
	k := NewKey()
	doc, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	var back Key
	if err := json.Unmarshal(doc, &back); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Public(), k.Public()) || !back.Valid() {
		t.Fatal("a kept key reads back as another")
	}
	if err := json.Unmarshal([]byte(`"AAAA"`), &back); err == nil {
		t.Error("a short seed reads as a key")
	}
	if _, err := json.Marshal(Key{}); err == nil {
		t.Error("an empty key marshals")
	}
	if (Key{}).Public() != nil || (Key{}).Valid() {
		t.Error("an empty key has a public half")
	}
}

// TestAPassAdmitsItsKeyUntilItExpires: a pass verifies under the term key it names
// within its life, through a frame's JSON, and is refused expired, from the future,
// under an unknown term or another authority, tampered with, or shown as a ticket.
func TestAPassAdmitsItsKeyUntilItExpires(t *testing.T) {
	authority, participant, other := NewKey(), NewKey(), NewKey()
	now := time.Unix(1_800_000_000, 0)
	body := PassBody{Session: "0123456789abcdef", Authority: authority.Public(), Key: participant.Public(),
		Kind: AdmitPlay, Account: "acct:7", Name: "lixen", Term: FirstTerm + 1}
	terms := func(term AuthorityTerm) []byte {
		if term == FirstTerm+1 {
			return authority.Public()
		}
		return nil
	}
	pass, err := SignPass(authority, body, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pass.Body), `"kind":"play"`) {
		t.Errorf("a kind travels by name: %s", pass.Body)
	}
	// Through the frame it travels in, and back.
	doc, _ := json.Marshal(AuthRequest{Key: participant.Public(), Pass: pass})
	var req AuthRequest
	if err := json.Unmarshal(doc, &req); err != nil {
		t.Fatal(err)
	}
	got, err := VerifyPass(req.Pass, terms, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Account != "acct:7" || got.Name != "lixen" || !bytes.Equal(got.Key, participant.Public()) ||
		got.Term != FirstTerm+1 || got.Expires-got.Issued != 3600 || got.Kind != AdmitPlay {
		t.Errorf("the pass read back is %+v", got)
	}
	if got.Renewable(now.Add(29*time.Minute)) || !got.Renewable(now.Add(31*time.Minute)) {
		t.Error("a pass renews past half its life")
	}
	capped, _ := SignPass(authority, body, now, 0)
	if c, err := VerifyPass(capped, terms, now); err != nil || time.Duration(c.Expires-c.Issued)*time.Second != parameter.PassLifetime {
		t.Errorf("a life of zero is the cap: %v %v", c.Expires-c.Issued, err)
	}
	tampered := *pass
	tampered.Body = []byte(strings.Replace(string(pass.Body), `"acct:7"`, `"acct:8"`, 1))
	asTicket := *pass
	for name, err := range map[string]error{
		"expired":            second(VerifyPass(pass, terms, now.Add(time.Hour+parameter.AuthClockLeeway+time.Second))),
		"from the future":    second(VerifyPass(pass, terms, now.Add(-parameter.AuthClockLeeway-time.Second))),
		"an unknown term":    second(VerifyPass(pass, func(AuthorityTerm) []byte { return nil }, now)),
		"another authority":  second(VerifyPass(pass, func(AuthorityTerm) []byte { return other.Public() }, now)),
		"a tampered body":    second(VerifyPass(&tampered, terms, now)),
		"shown as a ticket":  second(VerifyTicket(&asTicket, authority.Public(), now)),
		"no pass":            second(VerifyPass(nil, terms, now)),
		"an empty signature": second(VerifyPass(&Signed{Body: pass.Body}, terms, now)),
	} {
		if err == nil {
			t.Errorf("%s verifies", name)
		}
	}
	if _, err := SignPass(authority, PassBody{Authority: authority.Public(), Key: participant.Public(), Kind: AdmitPlay}, now, 0); err == nil {
		t.Error("a pass with no term is signed")
	}
	if _, err := SignPass(Key{}, body, now, 0); err == nil {
		t.Error("an empty key signs")
	}
}

// TestATicketAdmitsItsSubjectOnceWithinItsMinute: a ticket verifies under the
// router's key for TicketLifetime and its id is spent once; a door remembers at
// most its bound of spent ids and refuses a fresh one past it.
func TestATicketAdmitsItsSubjectOnceWithinItsMinute(t *testing.T) {
	router, participant := NewKey(), NewKey()
	now := time.Unix(1_800_000_000, 0)
	body := TicketBody{Session: "0123456789abcdef", Kind: AdmitPlay, Subject: SubjectOf(scriptedAddr), Key: participant.Public()}
	ticket, err := SignTicket(router, body, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyTicket(ticket, router.Public(), now.Add(parameter.TicketLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "addr:192.0.2.1" || got.ID == "" || !bytes.Equal(got.Key, participant.Public()) ||
		time.Duration(got.Expires-got.Issued)*time.Second != parameter.TicketLifetime {
		t.Errorf("the ticket read back is %+v", got)
	}
	again, _ := SignTicket(router, body, now)
	if a, _ := VerifyTicket(again, router.Public(), now); a.ID == got.ID {
		t.Error("two tickets share an id")
	}
	var spent SpentTickets
	if !spent.Spend(got, now) || spent.Spend(got, now) {
		t.Error("a ticket is spent once")
	}
	spent = SpentTickets{Max: 2}
	spent.Spend(got, now)
	third, _ := SignTicket(router, body, now)
	tb, _ := VerifyTicket(third, router.Public(), now)
	if !spent.Spend(tb, now) {
		t.Error("the second of two is refused")
	}
	fourth, _ := SignTicket(router, body, now)
	fb, _ := VerifyTicket(fourth, router.Public(), now)
	if spent.Spend(fb, now) {
		t.Error("a door past its bound spends more")
	}
	if !spent.Spend(fb, now.Add(parameter.TicketLifetime+parameter.AuthClockLeeway+time.Second)) {
		t.Error("expired ids are not swept")
	}
	for name, err := range map[string]error{
		"expired":         second(VerifyTicket(ticket, router.Public(), now.Add(parameter.TicketLifetime+parameter.AuthClockLeeway+time.Second))),
		"another key":     second(VerifyTicket(ticket, participant.Public(), now)),
		"shown as a pass": second(VerifyPass(ticket, func(AuthorityTerm) []byte { return router.Public() }, now)),
	} {
		if err == nil {
			t.Errorf("%s verifies", name)
		}
	}
	if _, err := SignTicket(router, TicketBody{Session: "x", Kind: AdmitPlay}, now); err == nil {
		t.Error("a ticket with no subject is signed")
	}
	if _, err := SignTicket(router, TicketBody{Session: strings.Repeat("n", MaxSessionName+1), Kind: AdmitPlay, Subject: "s"}, now); err == nil {
		t.Error("a ticket for an overlong name is signed")
	}
}

// TestAnAdmissionFrameFitsItsBound: a dialer's answer carrying a key, a proof, a
// pass and a ticket at their largest, and a door's question or refusal, encode
// under MaxAuthFrame; a refusal crosses as its kind.
func TestAnAdmissionFrameFitsItsBound(t *testing.T) {
	authority, participant := NewKey(), NewKey()
	now := time.Now()
	long := strings.Repeat("n", MaxSessionName)
	pass, err := SignPass(authority, PassBody{Session: long, Authority: authority.Public(), Key: participant.Public(),
		Kind: AdmitWatch, Account: strings.Repeat("a", maxSubject), Name: strings.Repeat("ß", 64), Term: 1 << 40}, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := SignTicket(authority, TicketBody{Session: long, Kind: AdmitPublish, Subject: strings.Repeat("s", maxSubject),
		Name: strings.Repeat("ß", 64), Key: participant.Public()}, now)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(AuthRequest{Key: participant.Public(), Proof: participant.SignProof(NewNonce(), long, ChannelBinding([]byte{1})),
		Pass: pass, Ticket: ticket, SignIn: strings.Repeat("t", 64), Scram: json.RawMessage(`{"full_nonce":"` + strings.Repeat("n", 512) + `"}`)})
	var refused AuthResponse
	refused.Refuse(&Refusal{Code: RefusalSignIn, Reason: "this door admits a pass, a ticket or a passphrase"})
	ans, _ := json.Marshal(refused)
	question, _ := json.Marshal(AuthResponse{Methods: []string{MethodKey, MethodPass, MethodTicket, MethodScram}, Nonce: NewNonce(),
		Scram: json.RawMessage(`{"salt":"` + strings.Repeat("s", 88) + `"}`)})
	for name, frame := range map[string][]byte{"an answer": req, "a refusal": ans, "a question": question} {
		if len(frame) > MaxAuthFrame {
			t.Errorf("%s is %d bytes, past %d", name, len(frame), MaxAuthFrame)
		}
	}
	var back AuthResponse
	if err := json.Unmarshal(ans, &back); err != nil || !errors.Is(back.Refused(), &Refusal{Code: RefusalSignIn}) {
		t.Errorf("a refusal crosses as %v", back.Refused())
	}
	if RetryOf(&Refusal{Code: RefusalCredential}) != RetryNever || RetryOf(&Refusal{Code: RefusalDropped}) != RetryNever ||
		RetryOf(&Refusal{Code: RefusalSignIn}) != RetryLater {
		t.Error("a refused credential or a dropped participant is retried")
	}
	var kind AdmissionKind
	if err := kind.UnmarshalText([]byte("owner")); err == nil || AdmissionKind(0).Valid() || AdmissionKind(9).String() != "kind(9)" {
		t.Error("an unknown kind reads as one")
	}
}

// FuzzASignedTokenIsOpenedWholeOrRefused: whatever bytes stand where a pass or a
// ticket does, verifying them under a key either yields the body that was signed or
// a refusal, never a panic, and a token that was signed still verifies.
func FuzzASignedTokenIsOpenedWholeOrRefused(f *testing.F) {
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	key := Key{private: ed25519.NewKeyFromSeed(seed)}
	now := time.Unix(1_800_000_000, 0)
	pass, _ := SignPass(key, PassBody{Session: "0123456789abcdef", Authority: key.Public(), Key: key.Public(), Kind: AdmitPlay, Term: FirstTerm}, now, time.Hour)
	ticket, _ := SignTicket(key, TicketBody{Session: "0123456789abcdef", Kind: AdmitPlay, Subject: "addr:192.0.2.1", Key: key.Public()}, now)
	for _, s := range []*Signed{pass, ticket} {
		doc, _ := json.Marshal(s)
		f.Add(doc)
		doc, _ = json.Marshal(AuthRequest{Pass: s, Ticket: s})
		f.Add(doc)
	}
	f.Add([]byte(`{"body":{"term":1},"sig":""}`))
	f.Add([]byte(`{"body":"not an object","sig":"AAAA"}`))
	f.Add([]byte(`{"body":{"term":-1}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, doc []byte) {
		var s Signed
		if json.Unmarshal(doc, &s) != nil {
			var req AuthRequest
			if json.Unmarshal(doc, &req) != nil || req.Pass == nil {
				return
			}
			s = *req.Pass
		}
		terms := func(AuthorityTerm) []byte { return key.Public() }
		if body, err := VerifyPass(&s, terms, now); err == nil && !bytes.Equal(body.Key, key.Public()) {
			t.Fatalf("a pass verified for another key: %+v", body)
		}
		if body, err := VerifyTicket(&s, key.Public(), now); err == nil && body.Subject != "addr:192.0.2.1" {
			t.Fatalf("a ticket verified for another subject: %+v", body)
		}
	})
}

// second is the error of a verification whose body a refusal test does not read.
func second[T any](_ T, err error) error { return err }
