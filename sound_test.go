package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

func TestSoundOnset(t *testing.T) {
	source := os.Getenv("SIXUAN_SOUND_TEST_SOURCE")
	if source == "" {
		t.Skip("requires explicit virtual sound output")
	}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stop := make(chan struct{})
		audio := make(chan []byte, 64)
		ready := make(chan struct{})
		go capture(ctx, stop, source, audio, func(e event) {
			if e.kind == "ready" {
				close(ready)
			}
		})
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("virtual audio not ready")
		}
		started := time.Now()
		done := make(chan struct{})
		go func() { playSound(makeCueSound(), .6, "start"); close(done) }()
		found := false
		deadline := time.After(3 * time.Second)
		for !found {
			select {
			case frame := <-audio:
				r, _ := levels(frame)
				if r > .005 {
					fmt.Printf("SOUND_ONSET_SECONDS %.6f\n", time.Since(started).Seconds())
					found = true
				}
			case <-deadline:
				cancel()
				t.Fatal("no audible cue")
			}
		}
		close(stop)
		for range audio {
		}
		cancel()
		<-done
		time.Sleep(100 * time.Millisecond)
	}
}
func makeCueSound() []byte {
	b := make([]byte, 24000/10*2)
	for i := 0; i < len(b)/2; i++ {
		value := int16(8192 * math.Sin(2*math.Pi*660*float64(i)/24000))
		binary.LittleEndian.PutUint16(b[2*i:], uint16(value))
	}
	return b
}
