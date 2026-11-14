# Keymap configuration

The complete runtime default is [`internal/asset/input/keymap.toml`](../asset/input/keymap.toml).
It is embedded into every native and WASM build, and `make install-config` installs
it as `input/keymap.toml` in the user config root (`$XDG_CONFIG_HOME/vif` on Linux),
or brings one already there up to date.

A document's keys override the embedded table, and `"none"` removes a binding. A
run brings the player's own up to date, adding each embedded binding of a key it
binds in no spelling, so it becomes a full copy that keeps its own bindings and
unbindings. Each key names one action, and several keys may name the same one
(`f2` below joins Ctrl+G and F10 on the menu). For example:

```toml
[normal]
h = "motion_right"
l = "motion_left"
space = "fire_main"
v = "none"

[normal_keys]
backspace = "undo"
f2 = "config_menu"

[prefix_g]
m = "motion_origin"
```

See [`doc/input-and-modes.md`](../../doc/input-and-modes.md) for sections,
actions, resolution order, and interaction semantics.
