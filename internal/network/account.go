package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/lixenwraith/auth"
)

// A site's account service, reached at its /vif/api/auth/ routes through the
// router: registration sends the credential this process derives, so the password
// never leaves it; a sign-in is the site's two-step SCRAM exchange, which ends in
// the token a dial shows the site's front door over TLS (Config.SignIn).
const (
	authRoute     = "/vif/api/auth/"
	accountReply  = 16 << 10
	accountDevice = 64
)

// The site stores a name in lowercase and keys the SCRAM credential by that exact
// spelling, so both exchanges spell it the same way, and a name the site would
// refuse is refused here before any KDF runs.
var accountNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func accountName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !accountNamePattern.MatchString(name) {
		return "", errors.New("an account name is 1 to 32 of a-z, 0-9, _ and -, starting with a letter")
	}
	return name, nil
}

// Register creates an account at site for name: the credential is derived here at
// the library's cost, the site's one profile, and holds the salt and the two keys
// alone (auth.Credential.Export).
func Register(site, name, password string, timeout time.Duration) error {
	name, err := accountName(name)
	if err != nil {
		return err
	}
	cred, err := auth.NewCredential(name, password)
	debug.FreeOSMemory()
	if err != nil {
		return err
	}
	var reply struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	body := map[string]any{"name": name, "credential": cred.Export()}
	return post(site+authRoute+"register", body, "", http.StatusCreated, &reply, timeout)
}

// SignIn runs the site's two-step login as name and returns the sign-in token for
// device; cookie asks the site to set it on this origin instead, for a page whose
// browser keeps it, and returns none. The KDF runs once, at the cost the site names.
func SignIn(site, name, password, device string, cookie bool, timeout time.Duration) (string, error) {
	if len(device) > accountDevice {
		device = device[:accountDevice]
	}
	if device == "" {
		device = "vif"
	}
	name, err := accountName(name)
	if err != nil {
		return "", err
	}
	client := auth.NewScramClient(name, password)
	first, err := client.StartAuthentication()
	if err != nil {
		return "", err
	}
	var challenge struct {
		Challenge *auth.ServerFirstMessage `json:"challenge"`
	}
	if err := post(site+authRoute+"login", map[string]any{"client_first": first}, "", http.StatusOK, &challenge, timeout); err != nil {
		return "", err
	}
	if challenge.Challenge == nil {
		return "", errors.New("the site answered no challenge")
	}
	proof, err := client.ProcessServerFirstMessage(*challenge.Challenge)
	debug.FreeOSMemory()
	if err != nil {
		return "", err
	}
	step := map[string]any{"proof": proof, "device": device}
	if cookie {
		step["session"] = "cookie"
	}
	var final struct {
		Final *auth.ServerFinalMessage `json:"final"`
		Token string                   `json:"token"`
	}
	if err := post(site+authRoute+"login", step, "", http.StatusOK, &final, timeout); err != nil {
		return "", err
	}
	if final.Final == nil || client.VerifyServerFinalMessage(*final.Final) != nil {
		return "", errors.New("the site did not prove it holds the account")
	}
	if !cookie && final.Token == "" {
		return "", errors.New("the site answered no sign-in")
	}
	return final.Token, nil
}

// SignOut revokes token at site; one the site no longer knows is no error.
func SignOut(site, token string, timeout time.Duration) error {
	err := post(site+authRoute+"logout", nil, token, http.StatusNoContent, nil, timeout)
	if err != nil && errors.Is(err, errUnauthorized) {
		return nil
	}
	return err
}

var errUnauthorized = errors.New("the site does not know this sign-in")

// post sends one JSON request and reads the answer into reply, or the site's
// refusal into an error; token rides as the bearer where given.
func post(url string, body any, token string, want int, reply any, timeout time.Duration) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, accountReply))
	if resp.StatusCode != want {
		var refusal struct {
			Error struct{ Message string } `json:"error"`
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return errUnauthorized
		}
		if json.Unmarshal(raw, &refusal) == nil && refusal.Error.Message != "" {
			return errors.New(refusal.Error.Message)
		}
		return fmt.Errorf("the site answered %s", resp.Status)
	}
	if reply != nil && len(raw) > 0 {
		return json.Unmarshal(raw, reply)
	}
	return nil
}
