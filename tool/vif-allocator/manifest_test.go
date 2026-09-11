package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildJobUsesFixedSessionShape(t *testing.T) {
	object := buildJob("abc123", workloadConfig{
		Namespace: "vif",
		Image:     "docker.io/library/vi-fighter:revision",
		Players:   4,
		MapSize:   "120x40",
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
		`"image":"docker.io/library/vi-fighter:revision"`,
		`"backoffLimit":0`,
		`"ttlSecondsAfterFinished":120`,
		`"-log-stdout"`,
		`"-first-join","90s"`,
		`"-empty","90s"`,
		`"-drain","20s"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Job JSON does not contain %s", want)
		}
	}
	for _, unwanted := range []string{`"logwisp"`, `"volumes"`, `"volumeMounts"`} {
		if strings.Contains(text, unwanted) {
			t.Errorf("stdout-only Job unexpectedly contains %s", unwanted)
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
