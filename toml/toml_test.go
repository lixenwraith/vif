package toml

import (
	"testing"
)

// TestUnmarshal_Complex verifies the full pipeline from TOML string to struct
// utilizing the latest generic decoding logic.
func TestUnmarshal_Complex(t *testing.T) {
	input := []byte(`
title = "Vi-Fighter Config"

[settings]
debug = true
max_fps = 144
scale = 1.5

[owner]
name = "Admin"
id = 55

[network]
hosts = ["10.0.0.1", "10.0.0.2"]
ports = [8080, 8081]

[[servers]]
name = "alpha"
active = true

[[servers]]
name = "beta"
active = false
`)

	type Settings struct {
		Debug  bool    `toml:"debug"`
		MaxFPS int     `toml:"max_fps"`
		Scale  float64 `toml:"scale"`
	}

	type Server struct {
		Name   string `toml:"name"`
		Active bool   `toml:"active"`
	}

	type Config struct {
		Title    string         `toml:"title"`
		Settings Settings       `toml:"settings"`
		Owner    map[string]any `toml:"owner"` // Test dynamic map
		Network  struct {
			Hosts []string `toml:"hosts"`
			Ports []int    `toml:"ports"`
		} `toml:"network"`
		Servers []Server `toml:"servers"` // Test Array of Tables
	}

	var cfg Config
	if err := Unmarshal(input, &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// 1. Basic Fields
	if cfg.Title != "Vi-Fighter Config" {
		t.Errorf("Title mismatch: got %q", cfg.Title)
	}

	// 2. Nested Struct & Types
	if !cfg.Settings.Debug {
		t.Error("Settings.Debug should be true")
	}
	if cfg.Settings.MaxFPS != 144 {
		t.Errorf("Settings.MaxFPS mismatch: got %d", cfg.Settings.MaxFPS)
	}
	if cfg.Settings.Scale != 1.5 {
		t.Errorf("Settings.Scale mismatch: got %f", cfg.Settings.Scale)
	}

	// 3. Dynamic Map (owner)
	if name, ok := cfg.Owner["name"].(string); !ok || name != "Admin" {
		t.Errorf("Owner.Name mismatch: got %v", cfg.Owner["name"])
	}
	// Check int conversion in dynamic map (parser returns int/float, decode handles struct fields, but map keeps raw parser types)
	// Parser likely returns int for 55.
	if id, ok := cfg.Owner["id"].(int); !ok || id != 55 {
		// Fallback check if parser returned generic float for number
		if fId, okf := cfg.Owner["id"].(float64); !okf || fId != 55 {
			t.Errorf("Owner.ID mismatch: got %T %v", cfg.Owner["id"], cfg.Owner["id"])
		}
	}

	// 4. Slices
	if len(cfg.Network.Hosts) != 2 || cfg.Network.Hosts[0] != "10.0.0.1" {
		t.Errorf("Network.Hosts mismatch: %v", cfg.Network.Hosts)
	}
	if len(cfg.Network.Ports) != 2 || cfg.Network.Ports[1] != 8081 {
		t.Errorf("Network.Ports mismatch: %v", cfg.Network.Ports)
	}

	// 5. Array of Tables
	if len(cfg.Servers) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "alpha" || !cfg.Servers[0].Active {
		t.Errorf("Server[0] mismatch: %+v", cfg.Servers[0])
	}
	if cfg.Servers[1].Name != "beta" || cfg.Servers[1].Active {
		t.Errorf("Server[1] mismatch: %+v", cfg.Servers[1])
	}
}

// TestDecode_RawPrimitives validates the reflection logic in decode.go
// specifically for type coercion (int -> float, int -> int64, etc.)
func TestDecode_RawPrimitives(t *testing.T) {
	// Simulate map[string]any output from Parser
	data := map[string]any{
		"int_val":   100,       // int
		"float_val": 123.45,    // float64
		"bool_val":  true,      // bool
		"str_val":   "hello",   // string
		"any_val":   "dynamic", // string -> any
	}

	type Target struct {
		Int   int64   `toml:"int_val"`   // Test int -> int64
		Float float32 `toml:"float_val"` // Test float64 -> float32
		Bool  bool    `toml:"bool_val"`
		Str   string  `toml:"str_val"`
		Any   any     `toml:"any_val"`
	}

	var tgt Target
	if err := Decode(data, &tgt); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if tgt.Int != 100 {
		t.Errorf("Int64 coercion failed: got %d", tgt.Int)
	}
	// Approximate float comparison
	if tgt.Float < 123.44 || tgt.Float > 123.46 {
		t.Errorf("Float32 coercion failed: got %f", tgt.Float)
	}
	if !tgt.Bool {
		t.Error("Bool failed")
	}
	if tgt.Str != "hello" {
		t.Error("String failed")
	}
	if tgt.Any != "dynamic" {
		t.Error("Any interface assignment failed")
	}
}

// TestDecode_NestedStructs tests direct Decode usage without Parser
func TestDecode_NestedStructs(t *testing.T) {
	// Nested map structure simulating [parent.child]
	data := map[string]any{
		"parent": map[string]any{
			"child": map[string]any{
				"val": 99,
			},
		},
	}

	type Child struct {
		Val int `toml:"val"`
	}
	type Parent struct {
		Child Child `toml:"child"`
	}
	type Top struct {
		Parent Parent `toml:"parent"`
	}

	var tgt Top
	if err := Decode(data, &tgt); err != nil {
		t.Fatalf("Decode nested failed: %v", err)
	}

	if tgt.Parent.Child.Val != 99 {
		t.Errorf("Nested decoding failed: got %d", tgt.Parent.Child.Val)
	}
}

// TestDecode_SliceCoercion tests converting []any (from parser) to specific slices
func TestDecode_SliceCoercion(t *testing.T) {
	data := map[string]any{
		"nums": []any{1, 2, 3},
	}

	type T struct {
		Nums []int `toml:"nums"`
	}

	var tgt T
	if err := Decode(data, &tgt); err != nil {
		t.Fatalf("Decode slice failed: %v", err)
	}

	if len(tgt.Nums) != 3 || tgt.Nums[2] != 3 {
		t.Errorf("Slice decoding failed: %v", tgt.Nums)
	}
}

// TestDecode_MapMap tests map[string]map[string]T
func TestDecode_MapMap(t *testing.T) {
	data := map[string]any{
		"config": map[string]any{
			"env": map[string]any{
				"production": true,
			},
		},
	}

	type T struct {
		Config map[string]map[string]bool `toml:"config"`
	}

	var tgt T
	if err := Decode(data, &tgt); err != nil {
		t.Fatalf("Decode map-map failed: %v", err)
	}

	if !tgt.Config["env"]["production"] {
		t.Error("Deep map decoding failed")
	}
}

// TestDecode_TargetValidation ensures non-pointer targets fail
func TestDecode_TargetValidation(t *testing.T) {
	var tgt struct{}
	err := Decode(map[string]any{}, tgt) // Pass by value (error)
	if err == nil {
		t.Error("Expected error when passing non-pointer to Decode")
	}

	var ptr *struct{} = nil
	err = Decode(map[string]any{}, ptr) // Pass nil pointer (error)
	if err == nil {
		t.Error("Expected error when passing nil pointer to Decode")
	}
}

// TestDecode_PrivateHelperAccess verifies toFloat functionality indirectly
// via Decode since we are in package toml
func TestDecode_TypeMismatch(t *testing.T) {
	data := map[string]any{
		"val": "not a number",
	}
	type T struct {
		Val int `toml:"val"`
	}
	var tgt T
	err := Decode(data, &tgt)
	if err == nil {
		t.Error("Expected error decoding string to int")
	}
}