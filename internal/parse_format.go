package internal

import (
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"strings"
)

// formatMessage 根据模板格式化消息
func formatMessage(template, logFile, timestamp, level string, content string, contentMaps []string) string {
	result := template

	// 收集带 “.” 替换为 “_”
	var replaceKeys = make(map[string]string)
	var replacementKeys = make([]string, 0)
	for _, v := range contentMaps {
		skey := v
		key := skey
		if strings.Contains(skey, ".") {
			nkey := strings.ReplaceAll(skey, ".", "_")
			replaceKeys[skey] = nkey
			key = nkey
			content = strings.ReplaceAll(content, skey, nkey)
		}
		replacementKeys = append(replacementKeys, key)
	}

	// 支持的占位符及其替换值
	replacements := map[string]string{}
	for _, k := range replacementKeys {
		key := "{" + k + "}"
		if key == "{file}" {
			replacements[key] = logFile
		} else if key == "{timestamp}" {
			replacements[key] = timestamp
		} else if key == "{level}" {
			replacements[key] = level
		} else {
			replacements[key] = gjson.Get(content, k).String()
		}
	}

	// 替换占位符
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}

	// 检查是否有未替换的占位符
	if strings.Contains(result, "{") {
		logrus.Warnf("Unknown placeholders in message template: %s", result)
	}

	return result
}
