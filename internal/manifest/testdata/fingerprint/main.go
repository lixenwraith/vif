// Command fingerprint prints this build's simulation fingerprint. It exists so a
// test can compare two processes: map iteration order is randomised per process,
// and a repeat inside one process would agree with itself regardless.
package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/internal/manifest"
)

func main() { fmt.Println(manifest.FingerprintString()) }
