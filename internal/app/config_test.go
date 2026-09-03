package app

import (
	"strings"
	"testing"
)

func TestNetworkSessionConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "host", cfg: Config{HostAddress: ":7777"}},
		{name: "join", cfg: Config{JoinAddress: "127.0.0.1:7777"}},
		{name: "later host cap", cfg: Config{Participants: 4}},
		{
			name: "guest cannot set host cap",
			cfg:  Config{JoinAddress: "127.0.0.1:7777", Participants: 4},
			want: "not a joining guest",
		},
		{
			name: "headless without script runner",
			cfg:  Config{Mode: ModeHeadless, HostAddress: ":7777", Width: 80, Height: 24},
			want: "RunScript",
		},
		{
			name: "headless script host",
			cfg: Config{
				Mode: ModeHeadless, HostAddress: ":7777", Width: 80, Height: 24,
				scriptedSession: true,
			},
		},
		{
			name: "exclusive",
			cfg:  Config{HostAddress: ":7777", JoinAddress: "127.0.0.1:7777"},
			want: "mutually exclusive",
		},
		{
			name: "replay cannot network",
			cfg:  Config{Mode: ModeReplay, HostAddress: ":7777", Width: 80, Height: 24},
			want: "playback cannot join",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHeadlessSessionRequiresTheScriptGate(t *testing.T) {
	t.Parallel()
	_, err := NewHeadless(Config{HostAddress: ":7777"})
	if err == nil || !strings.Contains(err.Error(), "RunScript") {
		t.Fatalf("NewHeadless() error = %v, want RunScript gate", err)
	}
}
