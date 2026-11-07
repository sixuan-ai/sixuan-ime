package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

var asrHTTPTransport http.RoundTripper = http.DefaultTransport

type openaiRecognizer struct{ settings asrConfig }

func pcmWAV(pcm []byte) []byte {
	b := make([]byte, 44, len(pcm)+44)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	return append(b, pcm...)
}
func (r *openaiRecognizer) recognize(ctx context.Context, audio <-chan []byte, _ func(string)) (string, error) {
	var pcm []byte
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case b, ok := <-audio:
			if !ok {
				return r.transcribe(ctx, pcm)
			}
			pcm = append(pcm, b...)
		}
	}
}
func (r *openaiRecognizer) transcribe(ctx context.Context, pcm []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, openaiTimeout)
	defer cancel()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	file, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err = file.Write(pcmWAV(pcm)); err != nil {
		return "", err
	}
	for k, v := range map[string]string{"model": r.settings.Model, "language": "zh", "response_format": "json"} {
		if err = w.WriteField(k, v); err != nil {
			return "", err
		}
	}
	if err = w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(r.settings.URL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", fmt.Errorf("ASR request URL invalid")
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if r.settings.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.settings.APIKey)
	}
	client := &http.Client{Transport: asrHTTPTransport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ASR HTTP request failed: %w", asrHTTPError(ctx))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		s := string(b)
		if r.settings.APIKey != "" {
			s = strings.ReplaceAll(s, r.settings.APIKey, "[redacted]")
		}
		return "", fmt.Errorf("ASR HTTP %d: %s", resp.StatusCode, s)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&result); err != nil {
		return "", err
	}
	return result.Text, nil
}
func asrHTTPError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("transport error")
}
