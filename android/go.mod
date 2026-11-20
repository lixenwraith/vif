module github.com/lixenwraith/vif/android

go 1.27.4

require (
	github.com/lixenwraith/terminal v0.6.7
	github.com/lixenwraith/vif v0.0.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/lixenwraith/ascimage v0.1.0 // indirect
	github.com/lixenwraith/audio v0.32.0 // indirect
	github.com/lixenwraith/auth v0.5.3 // indirect
	github.com/lixenwraith/color v0.1.5 // indirect
	github.com/lixenwraith/genetic v0.4.0 // indirect
	github.com/lixenwraith/log v0.1.19 // indirect
	github.com/lixenwraith/toml v0.10.0 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/mobile v0.0.0-20261117202452-47699113848a // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/term v0.48.0 // indirect
	golang.org/x/tools v0.53.0 // indirect
)

replace github.com/lixenwraith/vif => ../

tool (
	golang.org/x/mobile/cmd/gobind
	golang.org/x/mobile/cmd/gomobile
)
