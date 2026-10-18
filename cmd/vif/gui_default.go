//go:build vif_gui && (linux || freebsd) && !vif_headless && !vif_nogui

package main

// A vif_gui build is the desktop's: it opens the window unless -g=false
func init() { windowByDefault = true }
