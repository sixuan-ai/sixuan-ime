package main

import (
	"testing"
	"time"
)

func makeGesture() gesture { g := gesture{hold: time.Millisecond}; g.reset(); return g }
func TestHoldReleaseAndStop(t *testing.T) {
	g := makeGesture()
	g.feed("k", 29, 1, "idle")
	g.candidate.start = time.Now().Add(-time.Second)
	if g.check("idle") != "start" {
		t.Fatal("hold did not fire")
	}
	if g.feed("k", 29, 0, "recording") != "" {
		t.Fatal("first release stopped")
	}
	g.feed("k", 97, 1, "recording")
	if g.feed("k", 97, 0, "recording") != "stop" {
		t.Fatal("second tap did not stop")
	}
}
func TestChordsAndRepeats(t *testing.T) {
	g := makeGesture()
	g.feed("k", 29, 1, "idle")
	g.feed("k", 46, 1, "idle")
	g.candidate.start = time.Now().Add(-time.Second)
	if g.check("idle") != "" {
		t.Fatal("chord activated")
	}
	g.reset()
	g.feed("k", 29, 1, "idle")
	g.candidate.fired = true
	if g.feed("k", 46, 1, "recording") != "cancel" {
		t.Fatal("late chord not canceled")
	}
	if g.feed("k", 29, 2, "recording") != "" {
		t.Fatal("repeat")
	}
}
func TestDevicesAndBusy(t *testing.T) {
	g := makeGesture()
	g.feed("a", 42, 3, "idle")
	g.feed("b", 29, 1, "idle")
	if g.candidate != nil {
		t.Fatal("held modifier ignored")
	}
	g.reset()
	g.feed("k", 29, 1, "transcribing")
	if g.candidate != nil {
		t.Fatal("busy activation")
	}
	g.feed("k", -1, 0, "idle")
	if len(g.down) != 0 {
		t.Fatal("reset")
	}
}
