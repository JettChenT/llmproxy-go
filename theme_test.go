package main

import (
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

func TestLerpLab(t *testing.T) {
	black := labColor{0, 0, 0}
	white := labColor{100, 0, 0}

	// t=0 → black
	c := lerpLab(0, black, white)
	if c.L != 0 {
		t.Errorf("lerpLab(0) L = %f, want 0", c.L)
	}

	// t=1 → white
	c = lerpLab(1, black, white)
	if c.L != 100 {
		t.Errorf("lerpLab(1) L = %f, want 100", c.L)
	}

	// t=0.5 → midpoint
	c = lerpLab(0.5, black, white)
	if math.Abs(c.L-50) > 0.001 {
		t.Errorf("lerpLab(0.5) L = %f, want 50", c.L)
	}
}

func TestGenerate256Palette_Size(t *testing.T) {
	// Use a simple set of base16 colors
	var base16 [16]colorful.Color
	for i := 0; i < 16; i++ {
		base16[i] = colorful.Color{R: float64(i) / 15, G: float64(i) / 15, B: float64(i) / 15}
	}
	bg := colorful.Color{R: 0, G: 0, B: 0}
	fg := colorful.Color{R: 1, G: 1, B: 1}

	palette := generate256Palette(base16, bg, fg, false)

	// Palette must have exactly 256 entries
	if len(palette) != 256 {
		t.Fatalf("palette length = %d, want 256", len(palette))
	}
}

func TestGenerate256Palette_Base16Preserved(t *testing.T) {
	var base16 [16]colorful.Color
	for i := 0; i < 16; i++ {
		// Use distinct colors
		base16[i] = colorful.Color{
			R: float64(i%3) / 2,
			G: float64((i/3)%3) / 2,
			B: float64((i/6)%3) / 2,
		}
	}
	bg := colorful.Color{R: 0.05, G: 0.05, B: 0.05}
	fg := colorful.Color{R: 0.9, G: 0.9, B: 0.9}

	palette := generate256Palette(base16, bg, fg, false)

	// First 16 colors must be preserved exactly
	for i := 0; i < 16; i++ {
		if palette[i] != base16[i] {
			t.Errorf("palette[%d] = %v, want %v", i, palette[i], base16[i])
		}
	}
}

func TestGenerate256Palette_CubeCorners(t *testing.T) {
	// Standard-ish base16 colors
	base16 := [16]colorful.Color{
		{R: 0, G: 0, B: 0},       // 0: black
		{R: 0.8, G: 0, B: 0},     // 1: red
		{R: 0, G: 0.8, B: 0},     // 2: green
		{R: 0.8, G: 0.8, B: 0},   // 3: yellow
		{R: 0, G: 0, B: 0.8},     // 4: blue
		{R: 0.8, G: 0, B: 0.8},   // 5: magenta
		{R: 0, G: 0.8, B: 0.8},   // 6: cyan
		{R: 0.9, G: 0.9, B: 0.9}, // 7: white
	}
	// bright variants (8-15)
	for i := 8; i < 16; i++ {
		base16[i] = base16[i-8]
	}

	bg := colorful.Color{R: 0, G: 0, B: 0}
	fg := colorful.Color{R: 1, G: 1, B: 1}

	palette := generate256Palette(base16, bg, fg, false)

	// Color cube index (0,0,0) = 16 should be close to bg (black corner)
	c16 := palette[16]
	bgLab := rgbToLab(bg)
	c16Lab := rgbToLab(c16)
	dist := math.Sqrt(math.Pow(bgLab.L-c16Lab.L, 2) + math.Pow(bgLab.A-c16Lab.A, 2) + math.Pow(bgLab.B-c16Lab.B, 2))
	if dist > 1 {
		t.Errorf("palette[16] should be near bg, distance = %f", dist)
	}

	// Color cube index (5,5,5) = 231 should be close to fg (white corner)
	c231 := palette[231]
	fgLab := rgbToLab(fg)
	c231Lab := rgbToLab(c231)
	dist = math.Sqrt(math.Pow(fgLab.L-c231Lab.L, 2) + math.Pow(fgLab.A-c231Lab.A, 2) + math.Pow(fgLab.B-c231Lab.B, 2))
	if dist > 1 {
		t.Errorf("palette[231] should be near fg, distance = %f", dist)
	}
}

func TestGenerate256Palette_GrayscaleMonotonic(t *testing.T) {
	var base16 [16]colorful.Color
	for i := 0; i < 16; i++ {
		base16[i] = colorful.Color{R: float64(i) / 15, G: float64(i) / 15, B: float64(i) / 15}
	}
	bg := colorful.Color{R: 0, G: 0, B: 0}
	fg := colorful.Color{R: 1, G: 1, B: 1}

	palette := generate256Palette(base16, bg, fg, false)

	// Grayscale ramp (232-255) should have monotonically increasing lightness
	prevL := 0.0
	for i := 232; i < 256; i++ {
		l, _, _ := palette[i].Lab()
		if l < prevL-0.001 {
			t.Errorf("grayscale not monotonic at %d: L=%f < prevL=%f", i, l, prevL)
		}
		prevL = l
	}
}

func TestGenerate256Palette_LightThemeSwap(t *testing.T) {
	var base16 [16]colorful.Color
	for i := 0; i < 16; i++ {
		base16[i] = colorful.Color{R: 0.5, G: 0.5, B: 0.5}
	}
	// Light theme: bg is bright, fg is dark
	bg := colorful.Color{R: 1, G: 1, B: 1} // white bg
	fg := colorful.Color{R: 0, G: 0, B: 0} // black fg

	// Non-harmonious (default): should swap so cube still goes dark→light
	palette := generate256Palette(base16, bg, fg, false)
	l16, _, _ := palette[16].Lab()
	l231, _, _ := palette[231].Lab()
	if l16 > l231 {
		t.Errorf("non-harmonious light theme: palette[16] (L=%f) should be darker than palette[231] (L=%f)", l16, l231)
	}

	// Harmonious: cube goes light→dark (bg→fg direction)
	paletteH := generate256Palette(base16, bg, fg, true)
	l16h, _, _ := paletteH[16].Lab()
	l231h, _, _ := paletteH[231].Lab()
	if l16h < l231h {
		t.Errorf("harmonious light theme: palette[16] (L=%f) should be lighter than palette[231] (L=%f)", l16h, l231h)
	}
}

func TestDefaultTheme(t *testing.T) {
	theme := defaultTheme()
	// Smoke test: all colors should be non-empty strings
	colors := []lipgloss.Color{
		theme.Primary, theme.Secondary, theme.Accent, theme.Success,
		theme.Warning, theme.Error, theme.Dim, theme.Bg, theme.Surface,
		theme.Border, theme.Text,
	}
	for i, c := range colors {
		if string(c) == "" {
			t.Errorf("defaultTheme color %d is empty", i)
		}
	}
}
