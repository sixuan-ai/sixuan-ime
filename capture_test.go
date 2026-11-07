package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestCaptureIntegrity(t *testing.T) {
	source := os.Getenv("SIXUAN_TEST_SOURCE")
	output := os.Getenv("SIXUAN_TEST_CAPTURE")
	if source == "" || output == "" {
		t.Skip("requires explicit virtual microphone")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := make(chan struct{})
	audio := make(chan []byte, 3000)
	go capture(ctx, stop, source, audio, func(e event) {
		if e.kind == "ready" {
			fmt.Println("CAPTURE_READY")
		}
		if e.kind == "error" {
			t.Error(e.text)
		}
	})
	time.AfterFunc(8*time.Second, func() { close(stop) })
	f, e := os.Create(output)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	for b := range audio {
		if _, e = f.Write(b); e != nil {
			t.Fatal(e)
		}
	}
}
