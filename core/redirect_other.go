//go:build !unix

package core

import "errors"

var errNoDup = errors.New("core: fd redirection unsupported")

func dupToStderr(int) error  { return errNoDup }
func dupFD(int) (int, error) { return 0, errNoDup }
