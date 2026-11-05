//go:build vif_headless

package config

import "fmt"

func validateBuildConfig(cfg Config) error {
	if cfg.Mode.Presents() {
		return fmt.Errorf("%s: presentation is unavailable in a vif_headless build", cfg.Mode)
	}
	return nil
}
