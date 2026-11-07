package main

import (
	"fmt"
	"strings"
)

// Accept options before or after AUDIO, including --name=value.
func parseASRArgs(args []string) (string, func(*config), error) {
	values := map[string]string{}
	path := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			name, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if name != "provider" && name != "url" && name != "model" {
				return "", nil, fmt.Errorf("未知 asr 参数 --%s", name)
			}
			if !equals {
				i++
				if i >= len(args) {
					return "", nil, fmt.Errorf("--%s 缺少参数", name)
				}
				value = args[i]
			}
			values[name] = value
		} else {
			if path != "" {
				return "", nil, fmt.Errorf("usage: %s asr test AUDIO [--provider X --url U --model M]", configDirName)
			}
			path = arg
		}
	}
	if path == "" {
		return "", nil, fmt.Errorf("usage: %s asr test AUDIO [--provider X --url U --model M]", configDirName)
	}
	return path, func(c *config) {
		if v, ok := values["provider"]; ok {
			c.ASR.Provider = v
		}
		if v, ok := values["url"]; ok {
			c.ASR.URL = v
		}
		if v, ok := values["model"]; ok {
			c.ASR.Model = v
		}
	}, nil
}
