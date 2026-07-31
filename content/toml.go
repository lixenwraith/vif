package content

import (
	"fmt"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/core"
)

// SchemaVersion is the accepted value of the corpus schema key
const SchemaVersion = 1

type tomlCorpus struct {
	Schema int         `toml:"schema"`
	Blocks []tomlBlock `toml:"blocks"`
}

type tomlBlock struct {
	ID    string   `toml:"id"`
	Lines []string `toml:"lines"`
}

// parseTOML reads an authored corpus. Lines are literal: no comment stripping
// and no minimum length. Oversized blocks are split, never dropped.
func parseTOML(data []byte, p *Policy) ([]core.CodeBlock, error) {
	parser := toml.NewParser(data)
	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	var cfg tomlCorpus
	if err := toml.Decode(parsed, &cfg); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if cfg.Schema != SchemaVersion {
		return nil, fmt.Errorf("schema %d unsupported, want %d", cfg.Schema, SchemaVersion)
	}

	var (
		blocks  []core.CodeBlock
		scratch []byte
	)
	for _, b := range cfg.Blocks {
		lines := make([]string, 0, len(b.Lines))
		for _, raw := range b.Lines {
			text, _, s := sanitizeLine([]byte(raw), scratch, p)
			scratch = s
			if text != "" {
				lines = append(lines, text)
			}
		}
		for len(lines) > 0 {
			n := min(len(lines), p.MaxBlockLines)
			blocks = append(blocks, core.CodeBlock{Lines: lines[:n:n]})
			lines = lines[n:]
		}
	}

	return blocks, nil
}
