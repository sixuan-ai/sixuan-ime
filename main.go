package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	if handled, e := commonCLI(os.Args[1:]); handled {
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "asr" && os.Args[2] == "test" {
		os.Args = append(os.Args[:2], os.Args[3:]...)
	}
	if len(os.Args) > 1 && os.Args[1] == "asr" {
		path, overrides, e := parseASRArgs(os.Args[2:])
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		settings := readConfig(overrides)
		if settings.configError != "" {
			fmt.Fprintln(os.Stderr, settings.configError)
			os.Exit(1)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = context.WithValue(ctx, asrConfigContextKey{}, settings)
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-t", "60.001", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
		in, e := cmd.StdoutPipe()
		if e != nil {
			panic(e)
		}
		if e = cmd.Start(); e != nil {
			panic(e)
		}
		audio := make(chan []byte, 3000)
		go pcmReader(ctx, in, audio)
		start := time.Now()
		text, e := recognize(ctx, audio, func(event) {})
		if e != nil {
			cancel()
			cmd.Wait()
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if e = cmd.Wait(); e != nil {
			fmt.Fprintln(os.Stderr, "audio decoder failed")
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"text": text, "end_flag": 1, "transport": asrProvider(settings.ASR), "elapsed_seconds": time.Since(start).Seconds()})
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		if e := daemon(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		restoreService()
		return
	}
	action := "toggle"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}
	control(action)
}
