module github.com/lixenwraith/vif/android

go 1.27.2

require (
	github.com/lixenwraith/color v0.1.3
	github.com/lixenwraith/terminal v0.4.0
	github.com/lixenwraith/vif v0.0.0
)

require (
	github.com/lixenwraith/log v0.1.13 // indirect
	github.com/lixenwraith/toml v0.1.3 // indirect
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
