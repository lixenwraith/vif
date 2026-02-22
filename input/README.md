### Example config

```toml
# ~/.config/vi-fighter/keymap.toml
# Only overrides — unspecified keys retain defaults

[normal]
# Swap h/l (why not)
h = "motion_right"
l = "motion_left"

# Remap space to fire main instead of fire special
space = "fire_main"

# Unbind visual mode
v = "none"

[normal_keys]
# Remap backspace to undo
backspace = "undo"

[prefix_g]
# Remap gm to origin
m = "motion_origin"
```