module github.com/lixenwraith/vif/android

go 1.27.3

require (
	github.com/lixenwraith/terminal v0.6.7
	github.com/lixenwraith/vif v0.0.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/lixenwraith/ascimage v0.1.0 // indirect
	github.com/lixenwraith/audio v0.8.2 // indirect
	github.com/lixenwraith/auth v0.5.2 // indirect
	github.com/lixenwraith/color v0.1.5 // indirect
	github.com/lixenwraith/genetic v0.1.0 // indirect
	github.com/lixenwraith/log v0.1.13 // indirect
	github.com/lixenwraith/toml v0.6.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

replace github.com/lixenwraith/vif => ../

tool (
	golang.org/x/mobile/cmd/gobind
	golang.org/x/mobile/cmd/gomobile
)
