package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildJobUsesFixedSessionShape(t *testing.T) {
	object := buildJob("abc123", workloadConfig{
		Namespace: "vif",
		Image:     "docker.io/library/vif:revision",
		Players:   4,
		MapSize:   "120x40",
		Scenario:  "main",
		FirstJoin: "90s",
		Empty:     "90s",
		Drain:     "20s",
	})
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"name":"vif-session-abc123"`,
		`"image":"docker.io/library/vif:revision"`,
		`"backoffLimit":0`,
		`"ttlSecondsAfterFinished":120`,
		`"-l=/var/log/vif-fleet","-log-session-id=abc123"`,
		`{"mountPath":"/var/log/vif-fleet","name":"fleet-logs"}`,
		`{"mountPath":"/wad/scenario","name":"fleet-wad","readOnly":true,"subPath":"scenario"}`,
		`{"name":"fleet-wad","persistentVolumeClaim":{"claimName":"vif-fleet-wad","readOnly":true}}`,
		`"-config-dir","/wad","-s","main"`,
		`"args":["-check","-config-dir","/wad","-s","main"]`,
		`"envFrom":[{"configMapRef":{"name":"vif-session-env","optional":true}}]`,
		`"automountServiceAccountToken":false`,
		`"readOnlyRootFilesystem":true`,
		`"runAsNonRoot":true`,
		`"capabilities":{"drop":["ALL"]}`,
		`"seccompProfile":{"type":"RuntimeDefault"}`,
		`"-first-join","90s"`,
		`"-empty","90s"`,
		`"-drain","20s"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Job JSON does not contain %s", want)
		}
	}
	for _, unwanted := range []string{`"-log-stdout"`, `"hostPath"`, `"logwisp"`} {
		if strings.Contains(text, unwanted) {
			t.Errorf("file-logging Job unexpectedly contains %s", unwanted)
		}
	}
	if got := strings.Count(text, `"mountPath":"/var/log/vif-fleet"`); got != 1 {
		t.Errorf("fleet log volume is mounted %d times, want exactly once", got)
	}
}

// A bridge that failed would end the match if it were an ordinary container, and
// a fleet with no browser route must render the pod it rendered before there was
// one.
func TestTheBridgeIsARestartableSidecarAndOnlyWhenConfigured(t *testing.T) {
	base := workloadConfig{Namespace: "vif", Image: "vif:revision", Players: 4,
		MapSize: "120x40", Scenario: "main", FirstJoin: "90s", Empty: "90s", Drain: "20s"}
	plain, err := json.Marshal(buildJob("abc123", base))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "ws-bridge") {
		t.Fatal("a fleet with no browser route rendered a bridge")
	}

	// In initContainers and after the check, which is where a restartable container
	// is a sidecar rather than a step the pod waits for.
	base.BridgeImage = "ws-bridge:pinned"
	podSpec := buildJob("abc123", base)["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	inits := podSpec["initContainers"].([]any)
	if len(inits) != 2 {
		t.Fatalf("init containers = %d, want the check and the bridge", len(inits))
	}
	bridge := inits[1].(map[string]any)
	if bridge["name"] != "ws-bridge" || bridge["restartPolicy"] != "Always" {
		t.Fatalf("the bridge is not a sidecar after the check: %v", bridge)
	}
	encoded, err := json.Marshal(bridge)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"image":"ws-bridge:pinned"`,
		`"tcp:127.0.0.1:7777"`,
		`{"containerPort":7779,"name":"wsgame","protocol":"TCP"}`,
		`"readOnlyRootFilesystem":true`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("bridge JSON does not contain %s", want)
		}
	}
}

func TestBuildServiceOwnsJobAndPreservesSource(t *testing.T) {
	object := buildService("abc123", "job-uid", 31703, "vif")
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"name":"vif-session-abc123"`,
		`"uid":"job-uid"`,
		`"controller":true`,
		`"blockOwnerDeletion":true`,
		`"externalTrafficPolicy":"Local"`,
		`"nodePort":31703`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Service JSON does not contain %s", want)
		}
	}
}
