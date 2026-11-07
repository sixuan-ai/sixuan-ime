package main

import (
	"context"
	"strings"
)

const configureASRMessage = "请先配置语音识别：" + officialASRSetupInstructions

func asrProvider(c asrConfig) string { return c.Provider }
func validateASR(c *config) {
	switch c.ASR.Provider {
	case "":
		c.configError = configureASRMessage
		return
	case "sixuan", "funasr", "openai":
	default:
		c.configError = "asr.provider 必须为 sixuan、funasr 或 openai"
		return
	}
	c.ASR.URL = effectiveASRURL(c.ASR)
	if strings.TrimSpace(c.ASR.URL) == "" {
		c.configError = "asr.url 不能为空"
		return
	}
	if c.ASR.Provider == "openai" && strings.TrimSpace(c.ASR.Model) == "" {
		c.configError = "openai 的 asr.model 不能为空"
		return
	}
	validateASRKey(c)
}
func recognizeBuild(context.Context, <-chan []byte, func(event), config) (bool, string, error) {
	return false, "", nil
}
