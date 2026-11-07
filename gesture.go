package main

import "time"

type keyID struct {
	device string
	code   int
}
type candidate struct {
	key          keyID
	start        time.Time
	phase        string
	fired, chord bool
}
type gesture struct {
	down      map[keyID]bool
	candidate *candidate
	hold      time.Duration
}

func (g *gesture) reset() { g.down = map[keyID]bool{}; g.candidate = nil }
func (g *gesture) feed(device string, code, value int, phase string) string {
	if code < 0 {
		g.reset()
		return ""
	}
	if value == 2 {
		return ""
	}
	k := keyID{device, code}
	if value == 3 {
		g.down[k] = true
		return ""
	}
	if value == 1 {
		if g.down[k] {
			return ""
		}
		alone := len(g.down) == 0
		g.down[k] = true
		if (code == 29 || code == 97) && alone && (phase == "idle" || phase == "recording") {
			g.candidate = &candidate{key: k, start: time.Now(), phase: phase}
		} else if c := g.candidate; c != nil {
			c.chord = true
			if c.fired && c.phase == "idle" && phase == "recording" {
				return "cancel"
			}
		}
	} else if value == 0 {
		delete(g.down, k)
		if c := g.candidate; c != nil && c.key == k {
			g.candidate = nil
			if c.phase == "recording" && !c.chord && phase == "recording" {
				return "stop"
			}
		}
	}
	return ""
}
func (g *gesture) check(phase string) string {
	c := g.candidate
	if c != nil && c.phase == "idle" && !c.chord && !c.fired && phase == "idle" && len(g.down) == 1 && g.down[c.key] && time.Since(c.start) >= g.hold {
		c.fired = true
		return "start"
	}
	return ""
}
