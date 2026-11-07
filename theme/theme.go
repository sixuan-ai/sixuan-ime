// Package theme is the single source of appearance values for all desktop surfaces.
package theme

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Preset struct{ ID, Name, Accent, Base, Accent2 string }

var Presets = []Preset{
	{"neon", "荧光绿", "#3DFF8C", "#161A1D", ""},
	{"glacier", "冰川蓝", "#4CC9FF", "#121820", ""},
	{"sakura", "樱花粉", "#FF7EB6", "#1C1519", ""},
	{"amber", "琥珀", "#FFB238", "#1A1712", ""},
	{"lavender", "薰衣草", "#B69CFF", "#17151F", ""},
	{"cyber", "赛博", "#00F0FF", "#0D0B16", "#FF3EC8"},
	{"cinnabar", "朱砂红", "#FF5A5F", "#1A1213", ""},
	{"ink", "墨", "#EDEDED", "#111111", ""},
	{"cloud", "云白", "#16B364", "#EEF2F0", ""},
	{"latte", "奶茶", "#C9783B", "#F4ECE4", ""},
}

type Appearance struct {
	Accent2       string  `json:"accent2,omitempty"`
	Preset        string  `json:"preset"`
	Accent        string  `json:"accent"`
	Base          string  `json:"base"`
	Radius        string  `json:"radius"`
	Glow          string  `json:"glow"`
	CandidateSize string  `json:"candidate_size"`
	Opacity       float64 `json:"opacity"`
	warnings      []string
}

func Default() Appearance {
	return Appearance{Preset: "neon", Accent: Presets[0].Accent, Base: Presets[0].Base, Radius: "capsule", Glow: "soft", CandidateSize: "standard", Opacity: .94}
}

var colorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func Find(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// UnmarshalJSON isolates malformed appearance fields from unrelated preferences.
func (a *Appearance) UnmarshalJSON(b []byte) error {
	*a = Default()
	var raw map[string]json.RawMessage
	if string(b) == "null" {
		a.warnings = []string{"appearance 必须为对象"}
		return nil
	}
	if json.Unmarshal(b, &raw) != nil {
		a.warnings = []string{"appearance 必须为对象"}
		return nil
	}
	for k, v := range raw {
		var err error
		switch k {
		case "preset":
			err = json.Unmarshal(v, &a.Preset)
		case "accent":
			err = json.Unmarshal(v, &a.Accent)
		case "base":
			err = json.Unmarshal(v, &a.Base)
		case "accent2":
			err = json.Unmarshal(v, &a.Accent2)
		case "radius":
			err = json.Unmarshal(v, &a.Radius)
		case "glow":
			err = json.Unmarshal(v, &a.Glow)
		case "candidate_size":
			err = json.Unmarshal(v, &a.CandidateSize)
		case "opacity":
			err = json.Unmarshal(v, &a.Opacity)
		default:
			continue
		}
		if err != nil || string(v) == "null" {
			a.warnings = append(a.warnings, "appearance."+k+" 类型无效")
		}
	}
	// Preset-only documents select the preset's colors; explicit colors still win.
	if p, ok := Find(a.Preset); ok {
		if _, ok := raw["accent"]; !ok {
			a.Accent = p.Accent
		}
		if _, ok := raw["base"]; !ok {
			a.Base = p.Base
		}
	}
	*a, _ = Normalize(*a)
	return nil
}
func Normalize(a Appearance) (Appearance, []string) {
	d := Default()
	w := append([]string{}, a.warnings...)
	invalid := func(field string) { w = append(w, "appearance."+field+" 无效，已回退默认") }
	if _, ok := Find(a.Preset); !ok && a.Preset != "custom" {
		invalid("preset")
		a.Preset = d.Preset
	}
	if !colorRE.MatchString(a.Accent) {
		invalid("accent")
		a.Accent = d.Accent
	}
	if !colorRE.MatchString(a.Base) {
		invalid("base")
		a.Base = d.Base
	}
	if a.Accent2 != "" && !colorRE.MatchString(a.Accent2) {
		invalid("accent2")
		a.Accent2 = ""
	}
	a.Accent2 = strings.ToUpper(a.Accent2)
	a.Accent = strings.ToUpper(a.Accent)
	a.Base = strings.ToUpper(a.Base)
	if _, ok := Radii[a.Radius]; !ok {
		invalid("radius")
		a.Radius = d.Radius
	}
	if _, ok := Glows[a.Glow]; !ok {
		invalid("glow")
		a.Glow = d.Glow
	}
	if _, ok := Sizes[a.CandidateSize]; !ok {
		invalid("candidate_size")
		a.CandidateSize = d.CandidateSize
	}
	if math.IsNaN(a.Opacity) || math.IsInf(a.Opacity, 0) || a.Opacity < .7 || a.Opacity > 1 {
		invalid("opacity")
		a.Opacity = d.Opacity
	}
	a.warnings = w
	return a, w
}

var Radii = map[string][3]int{"capsule": {22, 16, 20}, "rounded": {14, 10, 14}, "square": {6, 4, 8}}
var Glows = map[string]float64{"off": 0, "soft": 1, "strong": 1.7}
var Sizes = map[string]int{"small": 14, "standard": 16, "large": 19}

type Tokens struct {
	Colors   map[string]string `json:"colors"`
	Radius   [3]int            `json:"radius"`
	Glow     float64           `json:"glow"`
	Size     int               `json:"size"`
	Opacity  float64           `json:"opacity"`
	Contrast float64           `json:"contrast"`
}

func RGB(h string) [3]float64 {
	var c [3]float64
	for i := range c {
		n, _ := strconv.ParseUint(h[1+i*2:3+i*2], 16, 8)
		c[i] = float64(n) / 255
	}
	return c
}
func Mix(a, b string, t float64) string {
	x, y := RGB(a), RGB(b)
	s := "#"
	for i := range x {
		s += fmt.Sprintf("%02X", int(math.Floor((x[i]*(1-t)+y[i]*t)*255+.5)))
	}
	return s
}
func Lum(h string) float64 {
	c := RGB(h)
	for i, v := range c {
		if v <= .03928 {
			c[i] = v / 12.92
		} else {
			c[i] = math.Pow((v+.055)/1.055, 2.4)
		}
	}
	return .2126*c[0] + .7152*c[1] + .0722*c[2]
}
func Ratio(a, b string) float64 {
	x, y := Lum(a), Lum(b)
	return (math.Max(x, y) + .05) / (math.Min(x, y) + .05)
}
func RGBA(h string, a float64) string {
	c := RGB(h)
	return fmt.Sprintf("rgba(%d,%d,%d,%s)", int(math.Round(c[0]*255)), int(math.Round(c[1]*255)), int(math.Round(c[2]*255)), strconv.FormatFloat(a, 'f', -1, 64))
}
func Derive(a Appearance) Tokens {
	a, _ = Normalize(a)
	x, b := a.Accent, a.Base
	v := map[string]string{"accent": x, "accent-hi": Mix(x, "#FFFFFF", .28), "accent-2": Mix(x, "#000000", .12), "on-accent": "#FFFFFF", "glow": RGBA(x, .42), "base": b}
	if p, ok := Find(a.Preset); ok && p.Accent2 != "" {
		v["accent-2"] = p.Accent2
	}
	if a.Preset == "custom" && a.Accent2 != "" {
		v["accent-2"] = a.Accent2
	}
	if Lum(x) >= .45 {
		v["on-accent"] = Mix(x, "#000000", .88)
	}
	if Lum(b) < .5 {
		v["raised-1"] = Mix(b, "#FFFFFF", .04)
		v["raised-2"] = Mix(b, "#000000", .18)
		v["sunken"] = Mix(b, "#000000", .38)
		v["header"] = Mix(b, "#000000", .28)
		v["text"] = Mix("#E8F0EA", x, .06)
		v["text-2"] = Mix(v["text"], b, .38)
		v["text-3"] = Mix(v["text"], b, .58)
		v["line"] = "rgba(255,255,255,.06)"
		v["nm-light"] = "rgba(255,255,255,.045)"
		v["nm-dark"] = "rgba(0,0,0,.55)"
		v["desk"] = "#202428"
	} else {
		v["raised-1"] = Mix(b, "#FFFFFF", .6)
		v["raised-2"] = Mix(b, "#000000", .04)
		v["sunken"] = Mix(b, "#000000", .09)
		v["header"] = Mix(b, "#000000", .05)
		v["text"] = "#1B2420"
		v["text-2"] = Mix(v["text"], b, .38)
		v["text-3"] = Mix(v["text"], b, .55)
		v["line"] = "rgba(0,0,0,.07)"
		v["nm-light"] = "rgba(255,255,255,.7)"
		v["nm-dark"] = "rgba(0,0,0,.12)"
		v["desk"] = "#D9DEDB"
	}
	return Tokens{Colors: v, Radius: Radii[a.Radius], Glow: Glows[a.Glow], Size: Sizes[a.CandidateSize], Opacity: a.Opacity, Contrast: Ratio(x, b)}
}

// AtomicWrite keeps readers from observing truncated stylesheets, including rename-based monitors.
func AtomicWrite(path string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".theme-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}

// Editable clears diagnostics from the parsed document before an explicit UI edit.
func (a Appearance) Editable() Appearance { a.warnings = nil; return a }
