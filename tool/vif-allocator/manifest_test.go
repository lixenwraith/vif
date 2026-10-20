package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildJobUsesFixedSessionShape(t *testing.T) {
	cfg := workloadConfig{
		Namespace: "vif",
		Image:     "docker.io/library/vif:revision",
		Players:   4,
		Size:      "120x40",
		Scenario:  "main",
		FirstJoin: "90s",
		Empty:     "90s",
		Drain:     "20s",
	}
	encoded, err := json.Marshal(buildJob("abc123", cfg))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"name":"vif-session-abc123"`,
		`"image":"docker.io/library/vif:revision"`,
		`"backoffLimit":0`,
		`"ttlSecondsAfterFinished":120`,
		`"-name","abc123"`,
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
		`"-size","120x40"`,
		`"annotations":{"vif.lixenwraith.dev/scenario":"main","vif.lixenwraith.dev/size":"120x40"}`,
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
	// First passes no -size, which is what hands the map to the first person.
	first := cfg
	first.Size = sizeFirst
	if encoded, err := json.Marshal(buildJob("abc123", first)); err != nil || strings.Contains(string(encoded), `"-size"`) {
		t.Errorf("a first-guest Job names a -size (err %v)", err)
	}
	// A bot grows the session container by a seat's cost; none leaves it as it was.
	for bots, want := range map[int][]string{
		0: {`{"name":"GOMEMLIMIT","value":"448MiB"}`,
			`"resources":{"limits":{"cpu":"500m","memory":"480Mi"},"requests":{"cpu":"100m","memory":"96Mi"}}`},
		3: {`{"name":"GOMEMLIMIT","value":"544MiB"}`,
			`"resources":{"limits":{"cpu":"500m","memory":"576Mi"},"requests":{"cpu":"292m","memory":"192Mi"}}`},
	} {
		cfg.Bots = bots
		encoded, err := json.Marshal(buildJob("abc123", cfg))
		if err != nil {
			t.Fatal(err)
		}
		for _, envelope := range want {
			if !strings.Contains(string(encoded), envelope) {
				t.Errorf("a Job with %d bots does not contain %s", bots, envelope)
			}
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
