//go:build vif_headless

package config

import "fmt"

func validateBuildConfig(cfg Config) error {
	if cfg.Mode.Presents() {
		return fmt.Errorf("%s: presentation unavailable in this build", cfg.Mode)
	}
	return nil
}
