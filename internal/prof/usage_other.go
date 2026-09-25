//go:build !unix

package prof

func readUsage() usage { return usage{} }
