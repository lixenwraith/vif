package content

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/core"
)

// Load reads every eligible file at the root of fsys into an immutable corpus.
// Per-file failures are recorded as rejections; only a directory read fails hard.
func Load(fsys fs.FS, p Policy) (*Corpus, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	c := &Corpus{}
	for _, e := range entries {
		if len(c.Sources) >= p.MaxFiles || c.Bytes >= p.MaxCorpusBytes {
			c.reject("*", "corpus budget exhausted")
			break
		}

		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ExtText && ext != ExtTOML {
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() > p.MaxFileBytes {
			c.reject(name, "exceeds max file size")
			continue
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			c.reject(name, "read: "+err.Error())
			continue
		}
		if !utf8.Valid(data) {
			c.reject(name, "invalid utf-8")
			continue
		}

		var blocks []core.CodeBlock
		if ext == ExtTOML {
			if blocks, err = parseTOML(data, &p); err != nil {
				c.reject(name, err.Error())
				continue
			}
		} else {
			blocks = parseText(data, &p)
		}
		if len(blocks) == 0 {
			c.reject(name, "no usable blocks")
			continue
		}

		lines := 0
		for _, b := range blocks {
			lines += len(b.Lines)
		}
		c.Sources = append(c.Sources, Source{Name: name, Blocks: blocks, Lines: lines})
		c.Bytes += int64(len(data))
	}

	return c, nil
}
