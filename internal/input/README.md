# Keymap configuration

The complete runtime default is [`default_keymap.toml`](default_keymap.toml).
It is embedded into every native and WASM build and installed as
`$XDG_CONFIG_HOME/vi-fighter/input/keymap.toml` by `make install-config`.

User documents may remain sparse: only supplied keys override the embedded
table, and `"none"` removes a binding. For example:

```toml
[normal]
h = "motion_right"
l = "motion_left"
space = "fire_main"
v = "none"

[normal_keys]
backspace = "undo"

[prefix_g]
m = "motion_origin"
```

See [`doc/input-and-modes.md`](../../doc/input-and-modes.md) for sections,
actions, resolution order, and interaction semantics.
