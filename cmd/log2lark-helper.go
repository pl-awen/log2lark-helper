package main

import (
	"flag"
	"github.com/sirupsen/logrus"
	"monitoring-log-reporting-to-lark/internal"
	"os"
	"strconv"
	"strings"
)

func main() {
	// 解析命令行参数
	logFiles := flag.String("log-files", "", "Comma-separated log file paths (e.g., /var/log/app1.log,/var/log/app2.log)")
	watchDirs := flag.String("watch-dirs", "", "The comma-separated path of the listening directory (e.g., /var/log1/,/var/log2/)")
	watchDirFileSuffix := flag.String("watch-dir-file-suffix", ".log", "Listen for the file suffixes in the directory (e.g., .log,.logger)")
	webhookURL := flag.String("webhook-url", "", "Webhook URL")
	webhookSecret := flag.String("webhook-secret", "", "Webhook secret key")
	cacheTimeSecond := flag.String("cache-time-second", "1", "Cache tine, in seconds")
	cacheContentIndex := flag.String("cache-content-index", "msg", "Cache content")
	matchRule := flag.String("match", "ERROR", "Regular expression to match log level")
	offsetFile := flag.String("offset-file", "monitoring_offset.json", "File to store log offsets")
	logRegex := flag.String("log-regex", `^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d{3})\s+(\w+)\s+(\{.*\})`, "Regular expression to parse log line")
	jsonPartContentIndex := flag.String("json-part-content-index", "#3", "Regular expressions match the content index in json format")
	levelFieldIndex := flag.String("level-field-index", "#2", "Level index of regular expression matching")
	messageFormat := flag.String("message-format", "🚨 错误日志告警\n文件: {file}\n时间: {#1}\n级别: {level}\n服务: {service_id}\n错误信息: {msg}\n调用者: {caller}\nTraceID: {trace_id}\nSpanID: {span_id}\n", "Message format template")
	lastStartLine := flag.String("last-start-line", "0", "The number of last starting lines separated by commas, corresponding to log-files (such as 100,200)")
	contentFields := flag.String("content-fields", "#1,#2,#3,service.id,msg,caller,trace.id,span.id", "Content fields")
	flag.Parse()

	if *logFiles == "" && *watchDirs == "" {
		logrus.Fatal("Either log-files or watch-dirs cannot be empty")
	}

	if *webhookURL == "" {
		logrus.Fatal("webhook-url is required")
	}

	var logFileList []string
	if *logFiles != "" {
		logFileList = strings.Split(*logFiles, ",")
	}

	// 解析起始行数
	var lastStartLines []int
	if *lastStartLine != "" {
		lineStrs := strings.Split(*lastStartLine, ",")
		for i, lineStr := range lineStrs {
			if i >= len(logFileList) {
				logrus.Warnf("The number of start-lines (%d) exceeds the number of log-files (%d). The extra lines will be ignored.", len(lineStrs), len(logFileList))
				break
			}
			lineNum, err := strconv.Atoi(strings.TrimSpace(lineStr))
			if err != nil {
				logrus.Fatalf("Invalid start-line value: %s (should be an integer)", lineStr)
			}
			lastStartLines = append(lastStartLines, lineNum)
		}
	}

	// 补齐 lastStartLines，长度与 logFileList 一致
	for len(lastStartLines) < len(logFileList) {
		lastStartLines = append(lastStartLines, 0)
	}

	// 解析监听目录
	var watchDirList []string
	if *watchDirs != "" {
		watchDirList = strings.Split(*watchDirs, ",")
		for i, dir := range watchDirList {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				logrus.Fatalf("Invalid watch-dir: Empty directory path")
			}

			if err := os.MkdirAll(dir, 0755); err != nil {
				logrus.Fatalf("The directory %s: %v cannot be created", dir, err)
			}
			watchDirList[i] = dir
		}
	}

	// 监听目录下的后续
	var watchDirFileSuffixList []string
	if *watchDirFileSuffix != "" {
		watchDirFileSuffixList = strings.Split(*watchDirFileSuffix, ",")
	}

	rrTimeSecond, err := strconv.Atoi(*cacheTimeSecond)
	if err != nil {
		logrus.Errorf("Invalid report-rate value: %s (should be an integer)", *cacheTimeSecond)
		return
	}

	app := internal.NewApp(internal.AppConfig{
		LogFiles:               logFileList,
		WatchDirs:              watchDirList,
		WatchDirFileSuffixList: watchDirFileSuffixList,
		WebhookURL:             *webhookURL,
		WebhookSecret:          *webhookSecret,
		MatchRule:              *matchRule,
		OffsetFile:             *offsetFile,
		LogRegex:               *logRegex,
		JsonPartContentIndex:   *jsonPartContentIndex,
		LevelFieldIndex:        *levelFieldIndex,
		ContentMaps:            strings.Split(*contentFields, ","),
		MessageFormat:          *messageFormat,
		LastStartLines:         lastStartLines,
		CacheTimeSecond:        rrTimeSecond,
		CacheContentIndex:      *cacheContentIndex,
	})
	app.Start()

	logrus.Info("Log monitor stopped")
}
