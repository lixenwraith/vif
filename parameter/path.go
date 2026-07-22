package parameter

// Config file locations (Unix-only: FreeBSD, Linux)
const (
	// AppConfigDirName is the directory under os.UserConfigDir (XDG)
	AppConfigDirName = "vi-fighter"

	// GameConfigFile is the FSM entry-point filename
	GameConfigFile = "game.toml"

	// KeymapConfigFile is the keymap override filename
	KeymapConfigFile = "keymap.toml"

	// LocalConfigDir is the repo-local fallback config directory
	LocalConfigDir = "./config"

	MusicConfigFile = "music.toml"

	SoundConfigFile = "sounds.toml"
)
