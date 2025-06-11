package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/hpcloud/tail"
	"github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config 程序配置
type Config struct {
	LogFiles      []string
	WebhookURL    string
	WebhookSecret string
	MatchRule     string
	OffsetFile    string
	LogRegex      string
	TimeIndex     int
	LevelIndex    int
	ContentIndex  int
	MessageFormat string
}

// Start 启动
func Start(config Config) {
	levelRe, err := regexp.Compile(config.MatchRule)
	if err != nil {
		logrus.Fatalf("Invalid match rule: %v", err)
	}

	logRe, err := regexp.Compile(config.LogRegex)
	if err != nil {
		logrus.Fatalf("Invalid log regex: %v", err)
	}

	if config.TimeIndex < 1 || config.LevelIndex < 1 || config.ContentIndex < 1 {
		logrus.Fatal("Index values must be positive integers")
	}

	offsetStore := NewOffsetStore()
	if err := offsetStore.Load(config.OffsetFile); err != nil {
		logrus.Warnf("Failed to load offsets from %s: %v", config.OffsetFile, err)
	}

	sigManager := NewSignatureManager(config.WebhookSecret)

	logger := logrus.New()
	logger.Info("Starting log monitor...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 捕获信号以优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Received shutdown signal, stopping...")
		cancel()
		if err := offsetStore.Save(config.OffsetFile); err != nil {
			logger.Errorf("Failed to save offsets to %s: %v", config.OffsetFile, err)
		}
	}()

	// 启动监控
	var wg sync.WaitGroup
	for _, logFile := range config.LogFiles {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			monitorLogFile(ctx, file, config, levelRe, logRe, offsetStore, sigManager, logger)
		}(logFile)
	}

	wg.Wait()
}

// formatMessage 根据模板格式化消息
func formatMessage(template, logFile, timestamp, level string, entry LogEntry) string {
	result := template
	// 支持的占位符及其替换值
	replacements := map[string]string{
		"{file}":      logFile,
		"{timestamp}": timestamp,
		"{level}":     level,
		"{service}":   entry.ServiceID,
		"{msg}":       entry.Msg,
		"{caller}":    entry.Caller,
		"{trace_id}":  entry.TraceID,
		"{span_id}":   entry.SpanID,
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

// monitorLogFile 监控单个日志文件
func monitorLogFile(ctx context.Context, logFile string, config Config, levelRe, logRe *regexp.Regexp, offsetStore *OffsetStore, sigManager *SignatureManager, logger *logrus.Logger) {
	// 初始化 tail
	offset := offsetStore.Get(logFile)
	t, err := tail.TailFile(logFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Location:  &tail.SeekInfo{Offset: offset, Whence: os.SEEK_SET},
	})
	if err != nil {
		logger.Errorf("Failed to tail file %s: %v", logFile, err)
		return
	}
	defer t.Cleanup()

	logger.Infof("Monitoring file: %s", logFile)

	for {
		select {
		case <-ctx.Done():
			// 获取当前偏移量
			offset, err := t.Tell()
			if err != nil {
				logger.Errorf("Failed to get offset for %s: %v", logFile, err)
			} else {
				offsetStore.Set(logFile, offset)
			}
			if err := offsetStore.Save(config.OffsetFile); err != nil {
				logger.Errorf("Failed to save offsets for %s: %v", logFile, err)
			}
			return
		case line, ok := <-t.Lines:
			if !ok {
				logger.Warnf("Tail channel closed for %s", logFile)
				return
			}
			if line.Err != nil {
				logger.Errorf("Error reading line from %s: %v", logFile, line.Err)
				continue
			}

			// 更新偏移量
			offset, err := t.Tell()
			if err != nil {
				logger.Errorf("Failed to get offset for %s: %v", logFile, err)
				continue
			}
			offsetStore.Set(logFile, offset)

			// 解析日志行
			matches := logRe.FindStringSubmatch(line.Text)
			if len(matches) <= config.TimeIndex || len(matches) <= config.LevelIndex || len(matches) <= config.ContentIndex {
				logger.Warnf("Invalid log format in %s: %s (insufficient capture groups)", logFile, line.Text)
				continue
			}
			timestamp := matches[config.TimeIndex]
			level := matches[config.LevelIndex]
			jsonPart := matches[config.ContentIndex]

			// 匹配日志级别
			if !levelRe.MatchString(level) {
				continue
			}

			var entry LogEntry
			if err := json.Unmarshal([]byte(jsonPart), &entry); err != nil {
				logger.Errorf("Failed to parse JSON in %s: %v", logFile, err)
				continue
			}

			message := formatMessage(config.MessageFormat, logFile, timestamp, level, entry)
			if err := sendToLarkWithRetry(config.WebhookURL, message, sigManager); err != nil {
				logger.Errorf("Failed to send to Lark for %s: %v", logFile, err)
			} else {
				logger.Infof("Sent to Lark for %s", logFile)
			}
		}
	}
}

// sendToLarkWithRetry 发送消息到 Lark Webhook，支持重试
func sendToLarkWithRetry(webhookURL, message string, sigManager *SignatureManager) error {
	msg := LarkMessage{
		MsgType: "text",
	}
	msg.Content.Text = message

	// 获取签名（如果有密钥）
	timestamp, signature, err := sigManager.GetSignature()
	if err != nil {
		return fmt.Errorf("failed to generate signature: %w", err)
	}
	if signature != "" {
		msg.Timestamp = fmt.Sprintf("%d", timestamp)
		msg.Sign = signature
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	operation := func() error {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("POST", webhookURL, strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
		return nil
	}

	// 配置指数退避重试
	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 30 * time.Second
	return backoff.Retry(operation, backoff.WithContext(bo, context.Background()))
}
