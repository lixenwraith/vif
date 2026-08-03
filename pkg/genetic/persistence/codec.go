package persistence

import (
	"encoding/json"

	"github.com/lixenwraith/toml"
)

// Codec serializes population state. Embedders supply their own format
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	// Ext is the file extension including the leading dot
	Ext() string
}

// TOMLCodec is the default; selected when NewManager receives a nil codec
type TOMLCodec struct{}

func (TOMLCodec) Marshal(v any) ([]byte, error)   { return toml.Marshal(v) }
func (TOMLCodec) Unmarshal(b []byte, v any) error { return toml.Unmarshal(b, v) }
func (TOMLCodec) Ext() string                     { return ".toml" }

// JSONCodec is the stdlib alternative
type JSONCodec struct{}

func (JSONCodec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (JSONCodec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func (JSONCodec) Ext() string                     { return ".json" }
