package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

const officialCandidatesURL = "https://sixuanai.com/v1/ime/candidates"

var candidateHTTPClient = &http.Client{Timeout: 3500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type cloudHTTPError struct{ status int }

func (e cloudHTTPError) Error() string { return cloudErrorMessage(e) }
func cloudErrorMessage(err error) string {
	var status cloudHTTPError
	if errors.As(err, &status) {
		switch status.status {
		case 429:
			return "云候选请求较多，已使用本地候选"
		}
	}
	return "云候选暂不可用，已使用本地候选"
}
func queryCloud(ctx context.Context, input string, _ config) ([]cloudCandidate, error) {
	q := normalizePinyin(input)
	if !validPinyin(q) || len(q) < 4 || len(input) > 256 {
		return nil, errors.New("请输入 4 到 128 个拼音字母")
	}
	body, _ := json.Marshal(map[string]string{"query": input})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, officialCandidatesURL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("云候选请求失败")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := candidateHTTPClient.Do(req)
	if err != nil {
		return nil, errors.New("云候选连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, cloudHTTPError{response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return nil, errors.New("云候选响应无效")
	}
	var result struct {
		Candidates []cloudCandidate `json:"candidates"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Candidates) > 10 {
		return nil, errors.New("云候选响应无效")
	}
	rows := []cloudCandidate{}
	for _, row := range result.Candidates {
		if !validPhrase(q, row.Text) || len([]rune(row.Text)) > 128 {
			return nil, errors.New("云候选响应无效")
		}
		if row.Syllable == "" {
			row.Syllable = q
		}
		if normalizePinyin(row.Syllable) == q {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
func cloudCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("用法：sixuan-ime cloud status|enable|disable|query 拼音")
	}
	c := readConfig()
	switch args[0] {
	case "status":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"enabled": c.CloudCandidates, "url": officialCandidatesURL, "requires_api_key": false, "dictionary_sync": false})
	case "enable", "disable":
		enabled := args[0] == "enable"
		if err := editConfig(map[string]any{"cloud_candidates": enabled}); err != nil {
			return err
		}
		reloadRunningService()
		return nil
	case "query":
		if len(args) != 2 {
			return errors.New("用法：sixuan-ime cloud query 'ni hao'")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.CloudTimeoutMS)*time.Millisecond)
		defer cancel()
		rows, err := queryCloud(ctx, args[1], c)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	return errors.New("未知云候选命令")
}
