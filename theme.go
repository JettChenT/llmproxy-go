package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
	"golang.org/x/sys/unix"
)

// ThemeMode selects between hardcoded and base16-derived themes.
type ThemeMode int

const (
	ThemeDefault ThemeMode = iota
	ThemeBase16
)

// Theme holds all semantic colors used by the TUI.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
	Dim       lipgloss.Color
	Bg        lipgloss.Color
	Surface   lipgloss.Color
	Border    lipgloss.Color
	Text      lipgloss.Color
}

// defaultTheme returns the hardcoded cyberpunk-inspired theme.
func defaultTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("#00D9FF"),
		Secondary: lipgloss.Color("#FF6B6B"),
		Accent:    lipgloss.Color("#A855F7"),
		Success:   lipgloss.Color("#4ADE80"),
		Warning:   lipgloss.Color("#FBBF24"),
		Error:     lipgloss.Color("#F87171"),
		Dim:       lipgloss.Color("#6B7280"),
		Bg:        lipgloss.Color("#0F172A"),
		Surface:   lipgloss.Color("#1E293B"),
		Border:    lipgloss.Color("#334155"),
		Text:      lipgloss.Color("#E2E8F0"),
	}
}

// base16Theme queries the terminal for its base16 colors (0-15, fg, bg),
// generates a 256-color palette via CIELAB interpolation, writes the palette
// to the terminal, and returns a Theme that uses ANSI 256 color indices.
func base16Theme() (Theme, bool) {
	base16, fg, bg, err := queryTerminalBase16Colors()
	if err != nil {
		return Theme{}, false
	}

	palette := generate256Palette(base16, bg, fg, false)

	// Set terminal colors 16-255 via OSC 4
	setTerminalPalette(palette)

	// Map semantic colors to base16/ANSI indices.
	// The base 16 colors (0-15) are the user's chosen theme.
	// Colors 16-255 are our generated palette.
	//
	// For surface/border, pick from the grayscale ramp (232-255).
	// Shade 1 (233) ≈ 4% brightness — good for surface.
	// Shade 3 (235) ≈ 12% brightness — good for border.
	return Theme{
		Primary:   lipgloss.Color("14"), // bright cyan
		Secondary: lipgloss.Color("9"),  // bright red
		Accent:    lipgloss.Color("13"), // bright magenta
		Success:   lipgloss.Color("10"), // bright green
		Warning:   lipgloss.Color("11"), // bright yellow
		Error:     lipgloss.Color("9"),  // bright red
		Dim:       lipgloss.Color("8"),  // bright black
		Bg:        lipgloss.Color("0"),  // black
		Surface:   lipgloss.Color("233"),
		Border:    lipgloss.Color("236"),
		Text:      lipgloss.Color("15"), // bright white
	}, true
}

// ---------- 256-color palette generation (CIELAB) ----------

type labColor struct {
	L, A, B float64
}

func rgbToLab(c colorful.Color) labColor {
	l, a, b := c.Lab()
	return labColor{l, a, b}
}

func labToRGB(lab labColor) colorful.Color {
	return colorful.Lab(lab.L, lab.A, lab.B).Clamped()
}

func lerpLab(t float64, c1, c2 labColor) labColor {
	return labColor{
		L: c1.L + t*(c2.L-c1.L),
		A: c1.A + t*(c2.A-c1.A),
		B: c1.B + t*(c2.B-c1.B),
	}
}

// generate256Palette builds a 256-color palette from the user's base16 colors
// via trilinear CIELAB interpolation.
//
// base16: colors 0-15 as colorful.Color
// bg, fg: terminal background and foreground
// harmonious: if true, use correct fg/bg semantics; if false, swap for light themes
func generate256Palette(base16 [16]colorful.Color, bg, fg colorful.Color, harmonious bool) [256]colorful.Color {
	// Map the 8 normal colors to cube corners.
	// Corners: black(0), red(1), green(2), yellow(3), blue(4), magenta(5), cyan(6), white(7)
	base8Lab := [8]labColor{
		rgbToLab(bg),          // black corner → bg
		rgbToLab(base16[1]),   // red
		rgbToLab(base16[2]),   // green
		rgbToLab(base16[3]),   // yellow
		rgbToLab(base16[4]),   // blue
		rgbToLab(base16[5]),   // magenta
		rgbToLab(base16[6]),   // cyan
		rgbToLab(fg),          // white corner → fg
	}

	isLightTheme := base8Lab[7].L < base8Lab[0].L
	if isLightTheme && !harmonious {
		base8Lab[0], base8Lab[7] = base8Lab[7], base8Lab[0]
	}

	var palette [256]colorful.Color

	// Copy base16 as-is
	for i := 0; i < 16; i++ {
		palette[i] = base16[i]
	}

	// 216-color cube via trilinear interpolation
	for r := 0; r < 6; r++ {
		c0 := lerpLab(float64(r)/5, base8Lab[0], base8Lab[1])
		c1 := lerpLab(float64(r)/5, base8Lab[2], base8Lab[3])
		c2 := lerpLab(float64(r)/5, base8Lab[4], base8Lab[5])
		c3 := lerpLab(float64(r)/5, base8Lab[6], base8Lab[7])
		for g := 0; g < 6; g++ {
			c4 := lerpLab(float64(g)/5, c0, c1)
			c5 := lerpLab(float64(g)/5, c2, c3)
			for b := 0; b < 6; b++ {
				c6 := lerpLab(float64(b)/5, c4, c5)
				palette[16+36*r+6*g+b] = labToRGB(c6)
			}
		}
	}

	// 24-step grayscale ramp
	for i := 0; i < 24; i++ {
		t := float64(i+1) / 25.0
		lab := lerpLab(t, base8Lab[0], base8Lab[7])
		palette[232+i] = labToRGB(lab)
	}

	return palette
}

// ---------- Terminal color querying via OSC ----------

// queryTerminalBase16Colors queries the terminal for colors 0-15, foreground,
// and background using OSC escape sequences. Returns an error if the terminal
// does not respond.
func queryTerminalBase16Colors() (base16 [16]colorful.Color, fg, bg colorful.Color, err error) {
	term := os.Getenv("TERM")
	if strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "dumb") {
		return base16, fg, bg, fmt.Errorf("terminal %q does not support OSC queries", term)
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return base16, fg, bg, fmt.Errorf("cannot open /dev/tty: %w", err)
	}
	defer tty.Close()

	fd := int(tty.Fd())

	// Save terminal state and set raw-ish mode (no echo, no canonical)
	oldTermios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return base16, fg, bg, fmt.Errorf("cannot get termios: %w", err)
	}
	defer unix.IoctlSetTermios(fd, unix.TIOCSETA, oldTermios) //nolint:errcheck

	raw := *oldTermios
	raw.Lflag &^= unix.ECHO | unix.ICANON
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1 // 100ms timeout per byte
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return base16, fg, bg, fmt.Errorf("cannot set raw mode: %w", err)
	}

	readOSCResponse := func() (string, error) {
		var buf []byte
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			b := make([]byte, 1)
			n, _ := tty.Read(b)
			if n == 0 {
				continue
			}
			buf = append(buf, b[0])
			// OSC responses end with BEL (\a) or ST (\033\\)
			if b[0] == '\a' {
				return string(buf), nil
			}
			if len(buf) >= 2 && buf[len(buf)-2] == '\033' && buf[len(buf)-1] == '\\' {
				return string(buf), nil
			}
		}
		if len(buf) > 0 {
			return string(buf), nil
		}
		return "", fmt.Errorf("timeout reading OSC response")
	}

	parseRGB := func(resp string) (colorful.Color, error) {
		// Find "rgb:" in the response
		idx := strings.Index(resp, "rgb:")
		if idx < 0 {
			return colorful.Color{}, fmt.Errorf("no rgb: in response")
		}
		rgbPart := resp[idx+4:]
		// Strip trailing control chars
		rgbPart = strings.TrimRight(rgbPart, "\a\033\\")
		parts := strings.Split(rgbPart, "/")
		if len(parts) != 3 {
			return colorful.Color{}, fmt.Errorf("expected 3 components, got %d", len(parts))
		}
		var r, g, b float64
		for i, p := range parts {
			var val uint64
			_, err := fmt.Sscanf(p, "%x", &val)
			if err != nil {
				return colorful.Color{}, fmt.Errorf("cannot parse hex %q: %w", p, err)
			}
			// Normalize: 1-digit → /15, 2-digit → /255, 4-digit → /65535
			var norm float64
			switch len(p) {
			case 1:
				norm = float64(val) / 0xF
			case 2:
				norm = float64(val) / 0xFF
			case 4:
				norm = float64(val) / 0xFFFF
			default:
				norm = float64(val) / math.Pow(16, float64(len(p))) // fallback
			}
			switch i {
			case 0:
				r = norm
			case 1:
				g = norm
			case 2:
				b = norm
			}
		}
		return colorful.Color{R: r, G: g, B: b}, nil
	}

	// Query and drain: send query, read one response, drain leftover bytes
	queryColor := func(query string) (colorful.Color, error) {
		_, _ = tty.Write([]byte(query))
		resp, err := readOSCResponse()
		if err != nil {
			return colorful.Color{}, err
		}
		return parseRGB(resp)
	}

	// Query foreground (OSC 10) and background (OSC 11)
	fg, err = queryColor("\033]10;?\a")
	if err != nil {
		return base16, fg, bg, fmt.Errorf("cannot query foreground: %w", err)
	}
	bg, err = queryColor("\033]11;?\a")
	if err != nil {
		return base16, fg, bg, fmt.Errorf("cannot query background: %w", err)
	}

	// Query palette colors 0-15 (OSC 4;N;?)
	for i := 0; i < 16; i++ {
		c, err := queryColor(fmt.Sprintf("\033]4;%d;?\a", i))
		if err != nil {
			return base16, fg, bg, fmt.Errorf("cannot query color %d: %w", i, err)
		}
		base16[i] = c
	}

	return base16, fg, bg, nil
}

// ---------- Setting terminal palette via OSC 4 ----------

// setTerminalPalette writes colors 16-255 to the terminal using OSC 4.
// It also records the escape sequence to restore the palette on exit.
func setTerminalPalette(palette [256]colorful.Color) {
	var sb strings.Builder
	for i := 16; i < 256; i++ {
		c := palette[i]
		r, g, b := c.Clamped().RGB255()
		sb.WriteString(fmt.Sprintf("\033]4;%d;rgb:%02x/%02x/%02x\a", i, r, g, b))
	}
	fmt.Fprint(os.Stdout, sb.String())

	// Record that we modified the palette so we can restore on exit.
	paletteModified = true
}

// restoreTerminalPalette resets colors 16-255 to the terminal default.
// Terminals that support OSC 104 will restore the original colors.
func restoreTerminalPalette() {
	if !paletteModified {
		return
	}
	// OSC 104 without parameters resets all colors.
	// Some terminals only support per-index reset.
	// Try full reset first; fall back to per-index if needed.
	fmt.Fprint(os.Stdout, "\033]104\a")
	paletteModified = false
}

var paletteModified bool

// ---------- Theme initialization ----------

var currentTheme Theme

// initTheme initializes the global theme. Call before any style usage.
// Returns an error message if base16 was requested but failed (falls back to default).
func initTheme(mode ThemeMode) string {
	switch mode {
	case ThemeBase16:
		t, ok := base16Theme()
		if ok {
			currentTheme = t
			return ""
		}
		currentTheme = defaultTheme()
		return "Warning: could not query terminal colors, falling back to default theme"
	default:
		currentTheme = defaultTheme()
		return ""
	}
}
