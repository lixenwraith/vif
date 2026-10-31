package persistence

import "encoding/json"

// Codec serializes population state. Embedders supply their own format
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	// Ext is the file extension including the leading dot
	Ext() string
}

// JSONCodec is the default; selected when NewManager receives a nil codec
type JSONCodec struct{}

func (JSONCodec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (JSONCodec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func (JSONCodec) Ext() string                     { return ".json" }
