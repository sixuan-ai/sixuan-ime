package theme

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed settings.css.tmpl
var settingsTemplate string

//go:embed overlay.css.tmpl
var overlayTemplate string

//go:embed candidates.css.tmpl
var candidatesTemplate string

// ThemedClass marks the candidate popup while the generated stylesheet is loaded.
const ThemedClass = "sx-themed"

// RenderColors includes compatibility aliases for the shipped neon palette.
// Derive remains exactly the design's algorithm; the historical neon renderer
// used hand-tuned colors, which must stay stable for existing users.
func RenderColors(a Appearance) map[string]string {
	v := Derive(a).Colors
	v["error"] = "#FF5A6A"
	v["error-hi"] = "#FFB0B8"
	v["error-orb-hi"] = "rgba(100%,84%,85%,1)"
	if legacyNeon(a) {
		for k, c := range map[string]string{"text": "#E8F0EA", "text-2": "#9AA7A0", "text-3": "#66726C", "accent-hi": "#6BFFAA", "accent-2": "#22E07A", "on-accent": "#06210F", "raised-1": "#1A1F23", "raised-2": "#13171A", "header": "#101316", "sunken": "#0E1113"} {
			v[k] = c
		}
	}
	v["wave-hi"] = v["accent-hi"]
	v["orb-hi"] = v["accent-hi"]
	v["orb-idle-hi"] = v["text-3"]
	v["recording-muted"] = v["text-3"]
	v["orb-shadow-light"] = v["nm-light"]
	v["orb-shadow-dark"] = v["nm-dark"]
	if legacyNeon(a) {
		v["wave-hi"] = "#B8FF5C"
		v["orb-hi"] = "rgba(85%,100%,90%,1)"
		v["orb-idle-hi"] = "rgba(79%,83%,80%,1)"
		v["recording-muted"] = "#9AA7A0"
		v["orb-shadow-light"] = "rgba(255,255,255,.05)"
		v["orb-shadow-dark"] = "rgba(0,0,0,.6)"
	}
	return v
}
func CSS(a Appearance, surface string) string {
	a, _ = Normalize(a)
	t := Derive(a)
	v := RenderColors(a)
	v["candidate-scope"] = CandidateScope
	for _, radius := range []int{8, 10, 12, 14, 18, 26} {
		v[fmt.Sprintf("glow-%d", radius)] = fmt.Sprint(float64(radius) * t.Glow)
	}
	v["pop-radius"] = fmt.Sprint(t.Radius[0])
	v["item-radius"] = fmt.Sprint(t.Radius[1])
	v["card-radius"] = fmt.Sprint(t.Radius[2])
	pillRadius := t.Radius[0] + 6
	if legacyNeon(a) && a.Radius == "capsule" {
		pillRadius = 32
	}
	v["pill-radius"] = fmt.Sprint(pillRadius)
	v["size"] = fmt.Sprint(t.Size)
	v["glow-radius"] = fmt.Sprint(14 * t.Glow)
	v["candidate-bg"] = RGBA(v["base"], t.Opacity)
	v["candidate-glow"] = RGBA(v["accent"], .42*t.Glow)
	v["index-color"] = RGBA(v["on-accent"], .6)
	v["disabled-color"] = RGBA(v["text-2"], .35)
	v["overlay-top"] = RGBA(v["raised-1"], .84)
	v["overlay-bottom"] = RGBA(v["sunken"], .90)
	for _, alias := range renderAliases {
		color := alias.Legacy
		if alias.Token != "" {
			color = v[alias.Token]
			if alias.Alpha >= 0 {
				alpha := alias.Alpha
				if alias.Glow {
					alpha *= t.Glow
					if surface == "candidates" {
						alpha = .42 * t.Glow
					}
				}
				color = RGBA(color, alpha)
			}
		}
		if legacyNeon(a) {
			color = alias.Legacy
			// Keep the historic alpha at soft glow, scale it for off/strong.
			if alias.Glow && t.Glow != 1 {
				color = RGBA(v["accent"], alias.Alpha*t.Glow)
			}
			if alias.Token == "base" && alias.Alpha >= 0 {
				color = RGBA("#161B1E", a.Opacity)
			}
		} else if alias.Token == "base" && alias.Alpha >= 0 {
			color = RGBA(v["base"], a.Opacity)
		}
		v[alias.Name] = color
	}

	source := candidatesTemplate
	if surface == "settings" {
		source = settingsTemplate + settingsExtra(t, v) + candidatePreviewCSS(a)
	} else if surface == "overlay" {
		source = overlayTemplate
	}
	for k, c := range v {
		source = strings.ReplaceAll(source, "{{"+k+"}}", c)
	}
	// Metadata supports the source tag in St without a second color derivation.
	if surface == "candidates" {
		// St cascades equal-specificity custom stylesheets with the earliest
		// loaded winning, so the bundled default would mask this file. The
		// extension adds ThemedClass only while this stylesheet is loaded.
		source = strings.ReplaceAll(source, "."+CandidateScope, "."+CandidateScope+"."+ThemedClass)
		source = "/* source-color: " + sourceColor(a) + " */\n" + source
	}
	return source
}
func settingsExtra(t Tokens, v map[string]string) string {
	return fmt.Sprintf(`
.sixuan-settings .swatch{border:1px solid %s;border-radius:20px;min-width:52px;min-height:40px;padding:4px;background:%s;color:%s;}
.sixuan-settings .swatch:checked{border:2px solid %s;}
.sixuan-settings .color-button{border-radius:16px;background:%s;color:%s;padding:6px;}
.sixuan-settings .contrast-warning{color:#FFB238;font-size:11px;}
.sixuan-settings scale trough{min-height:6px;border:0;border-radius:3px;background:%s;box-shadow:inset 1px 1px 2px rgba(0,0,0,.45);}
.sixuan-settings scale highlight{min-height:6px;border:0;border-radius:3px;background-image:linear-gradient(90deg,%s,%s);}
.sixuan-settings scale slider{min-width:18px;min-height:18px;margin:-7px;border:0;border-radius:9px;background-image:linear-gradient(145deg,#FFFFFF,%s);box-shadow:0 2px 4px rgba(0,0,0,.45);}
.sixuan-settings button.ghost{min-height:0;padding:8px 6px;border:0;border-radius:12px;background:transparent;background-image:none;box-shadow:none;color:%s;font-size:13px;text-shadow:none;}
.sixuan-settings button.ghost label{color:%s;}
.sixuan-settings button.ghost:hover label{color:%s;}
.sixuan-settings .segments .segment{padding:6px 10px;}
`, v["line"], v["raised-1"], v["text-2"], v["accent"], v["sunken"], v["text"], v["sunken"], v["accent-2"], v["accent"], v["raised-1"], v["text-2"], v["text-2"], v["text"])
}

// Keyfile is the cgo/standalone-preview wire format; no color math lives in C.
func Keyfile(a Appearance) string {
	a, _ = Normalize(a)
	t := Derive(a)
	var b strings.Builder
	fmt.Fprintf(&b, "[theme]\npreset=%s\naccent=%s\nbase=%s\nradius=%s\nglow=%s\ncandidate_size=%s\nopacity=%g\nglow_k=%g\ncontrast=%g\n", a.Preset, a.Accent, a.Base, a.Radius, a.Glow, a.CandidateSize, a.Opacity, t.Glow, t.Contrast)
	if legacyNeon(a) {
		fmt.Fprintln(&b, "orb_glow_alpha=0.25")
	} else {
		fmt.Fprintln(&b, "orb_glow_alpha=0.42")
	}
	if Lum(a.Base) >= .5 {
		fmt.Fprintln(&b, "light=1")
	} else {
		fmt.Fprintln(&b, "light=0")
	}
	for k, v := range RenderColors(a) {
		if k == "glow" {
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	for i, p := range Presets {
		fmt.Fprintf(&b, "preset-%d-id=%s\npreset-%d-name=%s\npreset-%d-accent=%s\npreset-%d-base=%s\n", i, p.ID, i, p.Name, i, p.Accent, i, p.Base)
	}
	return b.String()
}

func sourceColor(a Appearance) string {
	if legacyNeon(a) {
		return "#7FE3A8"
	}
	return Derive(a).Colors["accent-hi"]
}

// GTK's live candidate preview consumes the production St stylesheet through a
// syntax adapter. Geometry, foregrounds, gradients, radii and glow stay shared.
func candidatePreviewCSS(a Appearance) string {
	source := CSS(a, "candidates")
	source = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(source, "")
	var out strings.Builder
	for _, block := range strings.Split(source, "}") {
		parts := strings.SplitN(block, "{", 2)
		if len(parts) != 2 {
			continue
		}
		selector, body := strings.TrimSpace(parts[0]), parts[1]
		selector = strings.ReplaceAll(selector, "."+CandidateScope+"."+ThemedClass, ".sixuan-settings .candidate-preview")
		selector = strings.ReplaceAll(selector, ":selected", ".selected")
		selector = strings.ReplaceAll(selector, ":insensitive", ":disabled")
		selector = strings.ReplaceAll(selector, "StIcon", "image")
		// GtkBoxes do not have St's spacing or BoxPointer arrow properties.
		body = strings.ReplaceAll(body, "-arrow-background-color:", "background-color:")
		body = strings.ReplaceAll(body, "-arrow-border-radius:", "border-radius:")
		body = strings.ReplaceAll(body, "-arrow-border-color:", "border-color:")
		body = strings.ReplaceAll(body, "-arrow-border-width:", "border-width:")
		var start, end string
		for _, declaration := range strings.Split(body, ";") {
			d := strings.SplitN(strings.TrimSpace(declaration), ":", 2)
			if len(d) != 2 {
				continue
			}
			if d[0] == "background-gradient-start" {
				start = strings.TrimSpace(d[1])
			}
			if d[0] == "background-gradient-end" {
				end = strings.TrimSpace(d[1])
			}
		}
		body = regexp.MustCompile(`(?m)(^|;)\s*(background-gradient-[a-z-]+|spacing|width|height|icon-size|-arrow-[a-z-]+|-boxpointer-gap)\s*:[^;]*`).ReplaceAllString(body, "$1")
		if start != "" && end != "" {
			body += ";background-image:linear-gradient(180deg," + start + "," + end + ");"
		}
		fmt.Fprintf(&out, "%s{%s}\n", selector, body)
	}
	v := RenderColors(a)
	fmt.Fprintf(&out, ".sixuan-settings .candidate-preview .candidate-box label{color:%s;}\n.sixuan-settings .candidate-preview .candidate-box.selected label{color:%s;}\n.sixuan-settings .candidate-preview .candidate-box label.candidate-index{color:%s;font-size:11px;}\n.sixuan-settings .candidate-preview .candidate-box label.source-tag{color:%s;font-size:10px;}\n", v["text"], v["on-accent"], v["text-3"], sourceColor(a))
	return out.String()
}

func legacyNeon(a Appearance) bool {
	return a.Preset == "neon" && a.Accent == Presets[0].Accent && a.Base == Presets[0].Base
}
