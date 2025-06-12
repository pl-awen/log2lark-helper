package main

import (
	"flag"
	"github.com/sirupsen/logrus"
	"monitoring-log-reporting-to-lark/internal"
	"strconv"
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
	contentFields := flag.String("content-fields", "file,timestamp,level,service.id,msg,caller,trace.id,span.id", "Content fields")
	messageFormat := flag.String("message-format", "🚨 错误日志告警\n文件: {file}\n时间: {timestamp}\n级别: {level}\n服务: {service_id}\n错误信息: {msg}\n调用者: {caller}\nTraceID: {trace_id}\nSpanID: {span_id}", "Message format template")
	startLine := flag.String("start-line", "", "The number of starting lines separated by commas, corresponding to log-files (such as 100,200)")

	flag.Parse()

	if *logFiles == "" || *webhookURL == "" {
		logrus.Fatal("log-files and webhook-url are required")
	}

	logFileList := strings.Split(*logFiles, ",")

	// 解析起始行数
	var startLines []int
	if *startLine != "" {
		lineStrs := strings.Split(*startLine, ",")
		for i, lineStr := range lineStrs {
			if i >= len(logFileList) {
				logrus.Warnf("The number of start-lines (%d) exceeds the number of log-files (%d). The extra lines will be ignored.", len(lineStrs), len(logFileList))
				break
			}
			lineNum, err := strconv.Atoi(strings.TrimSpace(lineStr))
			if err != nil {
				logrus.Fatalf("Invalid start-line value: %s (should be an integer)", lineStr)
			}
			startLines = append(startLines, lineNum)
		}
	}

	// 补齐 startLines，长度与 logFileList 一致
	for len(startLines) < len(logFileList) {
		startLines = append(startLines, 0)
	}

	app := internal.NewApp(internal.AppConfig{
		LogFiles:      logFileList,
		WebhookURL:    *webhookURL,
		WebhookSecret: *webhookSecret,
		MatchRule:     *matchRule,
		OffsetFile:    *offsetFile,
		LogRegex:      *logRegex,
		TimeIndex:     *timeIndex,
		LevelIndex:    *levelIndex,
		ContentIndex:  *contentIndex,
		ContentMaps:   strings.Split(*contentFields, ","),
		MessageFormat: *messageFormat,
		StartLines:    startLines,
	})
	app.Start()

	logrus.Info("Log monitor stopped")
}
