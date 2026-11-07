package main

import (
	"errors"
	"fmt"
)

// Keep protocol codes separate from server prose when choosing notifications.
type asrServiceError struct {
	code, message string
}

func (e *asrServiceError) Error() string { return fmt.Sprintf("ASR %s: %s", e.code, e.message) }

func asrNotificationMessage(err error) string {
	var serviceErr *asrServiceError
	if errors.As(err, &serviceErr) {
		switch serviceErr.code {
		case "quota_exceeded":
			return "今日语音额度已用完，北京时间 0 点重置"
		case "unauthorized":
			return "API Key 无效或已撤销，请在 sixuanai.com 账号页重新创建"
		case "unavailable":
			return "语音识别服务暂时不可用，请稍后再试"
		case "rate_limited":
			return "请求过于频繁，请稍后再试"
		}
	}
	return "语音识别失败：" + err.Error()
}
