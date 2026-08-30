package app

import (
	"strings"
	"testing"
)

func TestNetworkSessionConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "host", cfg: Config{HostAddress: ":7777"}},
		{name: "join", cfg: Config{JoinAddress: "127.0.0.1:7777"}},
		{
			name: "exclusive",
			cfg:  Config{HostAddress: ":7777", JoinAddress: "127.0.0.1:7777"},
			want: "mutually exclusive",
		},
		{
			name: "interactive only",
			cfg:  Config{Mode: ModeHeadless, HostAddress: ":7777", Width: 80, Height: 24},
			want: "interactive play mode",
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
