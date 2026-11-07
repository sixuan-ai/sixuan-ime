package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var asrWebsocketDialer = websocket.DefaultDialer

type websocketRecognizer struct {
	settings config
	sixuan   bool
}
type asrMessage struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
	IsFinal bool   `json:"is_final"`
}
type asrRead struct {
	message asrMessage
	err     error
}

func (r *websocketRecognizer) recognize(ctx context.Context, audio <-chan []byte, partial func(string)) (text string, err error) {
	defer func() {
		if ctx.Err() != nil {
			text = ""
			err = ctx.Err()
		}
		if err != nil && r.settings.ASR.APIKey != "" && strings.Contains(err.Error(), r.settings.ASR.APIKey) {
			if serviceErr, ok := err.(*asrServiceError); ok {
				err = &asrServiceError{code: strings.ReplaceAll(serviceErr.code, r.settings.ASR.APIKey, "[redacted]"), message: strings.ReplaceAll(serviceErr.message, r.settings.ASR.APIKey, "[redacted]")}
			} else {
				err = errors.New(strings.ReplaceAll(err.Error(), r.settings.ASR.APIKey, "[redacted]"))
			}
		}
	}()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	h := http.Header{}
	if r.settings.ASR.APIKey != "" {
		h.Set("Authorization", "Bearer "+r.settings.ASR.APIKey)
	}
	dialer := *asrWebsocketDialer
	dialer.HandshakeTimeout = sixuanReadyTimeout
	conn, resp, err := dialer.DialContext(ctx, r.settings.ASR.URL, h)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return "", errors.New("ASR websocket connection failed")
	}
	defer conn.Close()
	conn.SetReadLimit(1024 * 1024)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	writeJSON := func(v any) error {
		if e := conn.SetWriteDeadline(time.Now().Add(asrWriteTimeout)); e != nil {
			return e
		}
		return conn.WriteJSON(v)
	}
	var start any
	if r.sixuan {
		polishMode := r.settings.TextPolish
		if publicBuild {
			polishMode = "off"
		}
		start = struct {
			Type        string   `json:"type"`
			Format      string   `json:"format"`
			SampleRate  int      `json:"sample_rate"`
			Channels    int      `json:"channels"`
			Language    string   `json:"language"`
			Punctuation bool     `json:"punctuation"`
			Polish      string   `json:"polish"`
			Hotwords    []string `json:"hotwords,omitempty"`
		}{"start", "pcm_s16le", 16000, 1, "zh", true, polishMode, r.settings.ASR.Hotwords}
	} else {
		funStart := map[string]any{"mode": "2pass", "chunk_size": []int{5, 10, 5}, "chunk_interval": 10, "wav_name": "sixuan", "is_speaking": true, "wav_format": "pcm", "audio_fs": 16000, "itn": true}
		if len(r.settings.ASR.Hotwords) > 0 {
			words := map[string]int{}
			for _, w := range r.settings.ASR.Hotwords {
				words[w] = 20
			}
			b, _ := json.Marshal(words)
			funStart["hotwords"] = string(b)
		}
		start = funStart
	}
	started := make(chan error, 1)
	accepted := make(chan struct{})
	written := make(chan error, 1)
	var stopped atomic.Bool
	go func() {
		e := writeJSON(start)
		started <- e
		if e != nil {
			return
		}
		select {
		case <-accepted:
		case <-done:
			return
		case <-ctx.Done():
			return
		}
		pending := make([]byte, 0, 3200)
		flush := func() error {
			if len(pending) == 0 {
				return nil
			}
			conn.SetWriteDeadline(time.Now().Add(asrWriteTimeout))
			e := conn.WriteMessage(websocket.BinaryMessage, pending)
			pending = pending[:0]
			return e
		}
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case frame, ok := <-audio:
				if ok {
					pending = append(pending, frame...)
					if len(pending) >= 3200 {
						if e = flush(); e != nil {
							written <- e
							return
						}
					}
					continue
				}
				if e = flush(); e != nil {
					written <- e
					return
				}
				stop := map[string]any{"is_speaking": false}
				if r.sixuan {
					stop = map[string]any{"type": "stop"}
				}
				stopped.Store(true)
				written <- writeJSON(stop)
				return
			}
		}
	}()
	if err = <-started; err != nil {
		return "", err
	}

	if r.sixuan {
		conn.SetReadDeadline(time.Now().Add(sixuanReadyTimeout))
		for {
			var m asrMessage
			if err = conn.ReadJSON(&m); err != nil {
				return "", fmt.Errorf("ASR ready: %w", err)
			}
			if m.Type == "error" {
				return "", &asrServiceError{code: m.Code, message: m.Message}
			}
			if m.Type == "ready" {
				break
			}
		}
		conn.SetReadDeadline(time.Time{})
	}
	close(accepted)
	reads := make(chan asrRead)
	go func() {
		for {
			var m asrMessage
			e := conn.ReadJSON(&m)
			select {
			case reads <- asrRead{m, e}:
			case <-done:
				return
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timeout <-chan time.Time
	var stoppedAt, lastMessage time.Time
	confirmed, current := "", ""
	offline := false
	finish := func() (string, error) {
		s := confirmed + current
		if s == "" {
			return "", errors.New("empty recognition result")
		}
		return s, nil
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case e := <-written:
			if e != nil {
				return "", e
			}
			written = nil
			stoppedAt = time.Now()
			lastMessage = stoppedAt
			wait := funasrFinalTimeout
			if r.sixuan {
				wait = sixuanFinalTimeout
			}
			timer.Reset(wait)
			timeout = timer.C
		case got := <-reads:
			if got.err != nil {
				return "", fmt.Errorf("ASR read: %w", got.err)
			}
			m := got.message
			lastMessage = time.Now()
			if m.Type == "error" {
				return "", &asrServiceError{code: m.Code, message: m.Message}
			}
			if r.sixuan {
				switch m.Type {
				case "partial":
					partial(m.Text)
				case "final":
					if !stopped.Load() {
						return "", errors.New("ASR final before stop")
					}
					return m.Text, nil
				}
			} else {
				switch m.Mode {
				case "2pass-online", "online":
					current += m.Text
				case "2pass-offline", "offline":
					confirmed += m.Text
					current = ""
					offline = true
				}
				partial(confirmed + current)
				if m.IsFinal {
					if !stopped.Load() {
						return "", errors.New("ASR final before stop")
					}
					return finish()
				}
			}
		case <-timeout:
			if r.sixuan {
				return "", errors.New("ASR final timed out")
			}
			if !offline {
				return "", errors.New("ASR final timed out (no offline result)")
			}
			remaining := funasrIdleTimeout - time.Since(lastMessage)
			if time.Since(stoppedAt) >= funasrFinalTimeout && remaining <= 0 {
				return finish()
			}
			if remaining <= 0 {
				remaining = time.Millisecond
			}
			timer.Reset(remaining)
		}
	}
}
