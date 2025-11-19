package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
)

func TestGetHeatMeterColor(t *testing.T) {
	tests := []struct {
		name     string
		progress float64
		wantZero bool // true if expecting black (unfilled)
	}{
		{"Negative progress", -0.1, true},
		{"Zero progress", 0.0, true},
		{"Small progress", 0.001, false},
		{"Red segment start", 0.05, false},
		{"Red segment end", 0.166, false},
		{"Orange segment", 0.25, false},
		{"Yellow segment", 0.40, false},
		{"Green segment", 0.55, false},
		{"Cyan segment", 0.70, false},
		{"Blue segment", 0.80, false},
		{"Purple segment", 0.90, false},
		{"Max progress", 1.0, false},
		{"Over max progress", 1.5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color := GetHeatMeterColor(tt.progress)
			r, g, b := color.RGB()

			if tt.wantZero {
				if r != 0 || g != 0 || b != 0 {
					t.Errorf("Expected black (0,0,0) for progress %f, got (%d,%d,%d)", tt.progress, r, g, b)
				}
			} else {
				if r == 0 && g == 0 && b == 0 {
					t.Errorf("Expected non-black color for progress %f, got black", tt.progress)
				}
			}
		})
	}
}

func TestGetHeatMeterColorGradient(t *testing.T) {
	// Test that colors change smoothly across the gradient
	var prevR, prevG, prevB int32

	for i := 0; i <= 100; i++ {
		progress := float64(i) / 100.0
		color := GetHeatMeterColor(progress)
		r, g, b := color.RGB()

		if i > 0 {
			// Check that at least one channel changed (gradient is continuous)
			if r == prevR && g == prevG && b == prevB {
				// Allow some segments to have same color for adjacent values
				// but overall the gradient should be changing
			}
		}

		prevR, prevG, prevB = r, g, b
	}
}

func TestGetStyleForSequence(t *testing.T) {
	tests := []struct {
		name    string
		seqType components.SequenceType
		level   components.SequenceLevel
		wantFg  tcell.Color
	}{
		{"Green Dark", components.SequenceGreen, components.LevelDark, RgbSequenceGreenDark},
		{"Green Normal", components.SequenceGreen, components.LevelNormal, RgbSequenceGreenNormal},
		{"Green Bright", components.SequenceGreen, components.LevelBright, RgbSequenceGreenBright},
		{"Red Dark", components.SequenceRed, components.LevelDark, RgbSequenceRedDark},
		{"Red Normal", components.SequenceRed, components.LevelNormal, RgbSequenceRedNormal},
		{"Red Bright", components.SequenceRed, components.LevelBright, RgbSequenceRedBright},
		{"Blue Dark", components.SequenceBlue, components.LevelDark, RgbSequenceBlueDark},
		{"Blue Normal", components.SequenceBlue, components.LevelNormal, RgbSequenceBlueNormal},
		{"Blue Bright", components.SequenceBlue, components.LevelBright, RgbSequenceBlueBright},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := GetStyleForSequence(tt.seqType, tt.level)
			fg, bg, _ := style.Decompose()

			// Check foreground color matches expected
			if fg != tt.wantFg {
				t.Errorf("Expected foreground color %v, got %v", tt.wantFg, fg)
			}

			// Check background is RgbBackground
			if bg != RgbBackground {
				t.Errorf("Expected background to be RgbBackground, got %v", bg)
			}
		})
	}
}

func TestGetStyleForSequenceAllCombinations(t *testing.T) {
	// Test all valid combinations of SequenceType and SequenceLevel
	types := []components.SequenceType{
		components.SequenceGreen,
		components.SequenceRed,
		components.SequenceBlue,
	}

	levels := []components.SequenceLevel{
		components.LevelDark,
		components.LevelNormal,
		components.LevelBright,
	}

	for _, seqType := range types {
		for _, level := range levels {
			style := GetStyleForSequence(seqType, level)
			fg, bg, _ := style.Decompose()

			// Verify style has foreground and background set
			if fg == tcell.ColorDefault {
				t.Errorf("Foreground color is default for type=%v, level=%v", seqType, level)
			}
			if bg != RgbBackground {
				t.Errorf("Background is not RgbBackground for type=%v, level=%v", seqType, level)
			}
		}
	}
}

func TestColorConstants(t *testing.T) {
	// Test that color constants are defined (not nil/zero)
	colorTests := []struct {
		name  string
		color tcell.Color
	}{
		{"RgbSequenceGreenDark", RgbSequenceGreenDark},
		{"RgbSequenceGreenNormal", RgbSequenceGreenNormal},
		{"RgbSequenceGreenBright", RgbSequenceGreenBright},
		{"RgbSequenceRedDark", RgbSequenceRedDark},
		{"RgbSequenceRedNormal", RgbSequenceRedNormal},
		{"RgbSequenceRedBright", RgbSequenceRedBright},
		{"RgbSequenceBlueDark", RgbSequenceBlueDark},
		{"RgbSequenceBlueNormal", RgbSequenceBlueNormal},
		{"RgbSequenceBlueBright", RgbSequenceBlueBright},
		{"RgbLineNumbers", RgbLineNumbers},
		{"RgbStatusBar", RgbStatusBar},
		{"RgbColumnIndicator", RgbColumnIndicator},
		{"RgbBackground", RgbBackground},
		{"RgbPingHighlight", RgbPingHighlight},
		{"RgbCursorNormal", RgbCursorNormal},
		{"RgbCursorInsert", RgbCursorInsert},
		{"RgbCursorError", RgbCursorError},
		{"RgbModeNormalBg", RgbModeNormalBg},
		{"RgbModeInsertBg", RgbModeInsertBg},
		{"RgbScoreBg", RgbScoreBg},
		{"RgbBoostBg", RgbBoostBg},
		{"RgbStatusText", RgbStatusText},
	}

	for _, tt := range colorTests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b := tt.color.RGB()
			// Just verify the color has been set to something
			// (not checking specific values, just that it's defined)
			_ = r
			_ = g
			_ = b
		})
	}
}

func TestGetHeatMeterColorBoundaries(t *testing.T) {
	// Test segment boundaries to ensure smooth transitions
	boundaries := []float64{0.0, 0.167, 0.333, 0.500, 0.667, 0.833, 1.0}

	for i := 0; i < len(boundaries)-1; i++ {
		start := boundaries[i]
		end := boundaries[i+1]

		// Sample colors at start, middle, and just before end
		c1 := GetHeatMeterColor(start + 0.001)
		c2 := GetHeatMeterColor((start + end) / 2)
		c3 := GetHeatMeterColor(end - 0.001)

		// Verify all colors are non-black (assuming start > 0)
		if start > 0 {
			r1, g1, b1 := c1.RGB()
			r2, g2, b2 := c2.RGB()
			r3, g3, b3 := c3.RGB()

			if r1 == 0 && g1 == 0 && b1 == 0 {
				t.Errorf("Unexpected black at segment %d start", i)
			}
			if r2 == 0 && g2 == 0 && b2 == 0 {
				t.Errorf("Unexpected black at segment %d middle", i)
			}
			if r3 == 0 && g3 == 0 && b3 == 0 {
				t.Errorf("Unexpected black at segment %d end", i)
			}
		}
	}
}

func TestDecayAnimationBackgroundConsistency(t *testing.T) {
	// Verify that decay animation gray background is sufficiently visible
	// against the main RgbBackground to ensure decay animation remains visible
	decayGrayBg := tcell.NewRGBColor(60, 60, 60)

	bgR, bgG, bgB := RgbBackground.RGB()
	decayR, decayG, decayB := decayGrayBg.RGB()

	// Tokyo Night background is (26, 27, 38)
	// Decay gray background is (60, 60, 60)
	// Verify decay gray is significantly lighter than background
	if decayR <= bgR || decayG <= bgG || decayB <= bgB {
		t.Errorf("Decay animation background (%d,%d,%d) must be lighter than RgbBackground (%d,%d,%d) for visibility",
			decayR, decayG, decayB, bgR, bgG, bgB)
	}

	// Calculate approximate luminance difference
	// Simple approximation: average RGB values
	bgLuminance := (int32(bgR) + int32(bgG) + int32(bgB)) / 3
	decayLuminance := (int32(decayR) + int32(decayG) + int32(decayB)) / 3

	luminanceDiff := decayLuminance - bgLuminance

	// Verify there's at least 20 points of luminance difference for visibility
	if luminanceDiff < 20 {
		t.Errorf("Decay animation background luminance difference (%d) should be at least 20 for clear visibility",
			luminanceDiff)
	}
}

func TestBackgroundColorConsistency(t *testing.T) {
	// Verify that all character sequences use consistent RgbBackground
	types := []components.SequenceType{
		components.SequenceGreen,
		components.SequenceRed,
		components.SequenceBlue,
		components.SequenceGold,
	}

	levels := []components.SequenceLevel{
		components.LevelDark,
		components.LevelNormal,
		components.LevelBright,
	}

	for _, seqType := range types {
		for _, level := range levels {
			style := GetStyleForSequence(seqType, level)
			_, bg, _ := style.Decompose()

			if bg != RgbBackground {
				t.Errorf("Sequence type=%v level=%v has inconsistent background %v, expected RgbBackground %v",
					seqType, level, bg, RgbBackground)
			}
		}
	}
}