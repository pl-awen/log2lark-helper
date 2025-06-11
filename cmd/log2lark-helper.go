package main

import (
	"flag"
	"github.com/sirupsen/logrus"
	"monitoring-log-reporting-to-lark/internal"
	"strings"
)

func main() {
	// 解析命令行参数
	logFiles := flag.String("log-files", "", "Comma-separated log file paths (e.g., /var/log/app1.log,/var/log/app2.log)")
	webhookURL := flag.String("webhook-url", "", "Webhook URL")
	webhookSecret := flag.String("webhook-secret", "", "Webhook secret key")
	matchRule := flag.String("match", "ERROR", "Regular expression to match log level")
	offsetFile := flag.String("offset-file", "monitoring_offset.json", "File to store log offsets")
	logRegex := flag.String("log-regex", `^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d{3})\s+(\w+)\s+(\{.*\})`, "Regular expression to parse log line")
	timeIndex := flag.Int("time-index", 1, "Capture group index for timestamp")
	levelIndex := flag.Int("level-index", 2, "Capture group index for log level")
	contentIndex := flag.Int("content-index", 3, "Capture group index for content (JSON)")
	messageFormat := flag.String("message-format", "🚨 错误日志告警\n文件: {file}\n时间: {timestamp}\n级别: {level}\n服务: {service}\n错误信息: {msg}\n调用者: {caller}\nTraceID: {trace_id}\nSpanID: {span_id}", "Message format template")

	flag.Parse()

	if *logFiles == "" || *webhookURL == "" {
		logrus.Fatal("log-files and webhook-url are required")
	}

	// 解析日志文件路径
	config := internal.Config{
		LogFiles:      strings.Split(*logFiles, ","),
		WebhookURL:    *webhookURL,
		WebhookSecret: *webhookSecret,
		MatchRule:     *matchRule,
		OffsetFile:    *offsetFile,
		LogRegex:      *logRegex,
		TimeIndex:     *timeIndex,
		LevelIndex:    *levelIndex,
		ContentIndex:  *contentIndex,
		MessageFormat: *messageFormat,
	}

	internal.Start(config)
	logrus.Info("Log monitor stopped")
}
