package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/hpcloud/tail"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// AppConfig 程序配置
type AppConfig struct {
	LogFiles               []string
	WatchDirs              []string
	WatchDirFileSuffixList []string
	WebhookURL             string
	WebhookSecret          string
	LastStartLines         []int
	MatchRule              string
	OffsetFile             string
	LogRegex               string
	TimeIndex              int
	LevelIndex             int
	ContentIndex           int
	ContentMaps            []string
	MessageFormat          string
}

type App struct {
	config AppConfig
}

// NewApp ..
func NewApp(config AppConfig) *App {
	return &App{
		config: config,
	}
}

// Start 启动
func (app *App) Start() {
	levelRe, err := regexp.Compile(app.config.MatchRule)
	if err != nil {
		logrus.Fatalf("Invalid match rule: %v", err)
	}

	logRe, err := regexp.Compile(app.config.LogRegex)
	if err != nil {
		logrus.Fatalf("Invalid log regex: %v", err)
	}

	if app.config.TimeIndex < 1 || app.config.LevelIndex < 1 || app.config.ContentIndex < 1 {
		logrus.Fatal("Index values must be positive integers")
	}

	offsetStore := NewOffsetStore(app.config.OffsetFile)
	if err = offsetStore.Load(); err != nil {
		logrus.Warnf("Failed to load offsets from %s: %v", app.config.OffsetFile, err)
	}

	if err = offsetStore.checkFileExist(); err != nil {
		logrus.Warnf("Failed to check file offset from %s: %v", app.config.OffsetFile, err)
	}

	sigManager := NewSignatureManager(app.config.WebhookSecret)

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
		if err := offsetStore.Save(); err != nil {
			logger.Errorf("Failed to save offsets to %s: %v", app.config.OffsetFile, err)
		}
	}()

	// 发送飞书
	sendLarkManager := NewSendToLarkManager(app.config.WebhookURL, sigManager)

	// 创建监听管理
	mparms := MonitorParams{
		LevelRe:                levelRe,
		LogRe:                  logRe,
		WatchDirFileSuffixList: app.config.WatchDirFileSuffixList,
		TimeIndex:              app.config.LevelIndex,
		LevelIndex:             app.config.LevelIndex,
		ContentIndex:           app.config.ContentIndex,
		ContentMaps:            app.config.ContentMaps,
		MessageFormat:          app.config.MessageFormat,
	}
	monitorManager := NewMonitorManager(mparms, sendLarkManager, offsetStore, logger)

	// 启动文件监控
	var wg sync.WaitGroup
	for i, logFile := range app.config.LogFiles {
		wg.Add(1)
		go func(file string, lastStartLine int) {
			defer wg.Done()
			monitorManager.StartFileMonitor(ctx, file, lastStartLine)
		}(logFile, app.config.LastStartLines[i])
	}

	// 启动目录监听
	for _, dir := range app.config.WatchDirs {
		wg.Add(1)
		go func(ctx context.Context, directory string, wg *sync.WaitGroup) {
			defer wg.Done()
			monitorManager.StartWatchDirectory(ctx, directory, wg)
		}(ctx, dir, &wg)
	}

	logrus.Info("Log monitor started")
	wg.Wait()
	if err = offsetStore.Save(); err != nil {
		logrus.Info("Save offsets to file failed: %v", err)
	}
}

// formatMessage 根据模板格式化消息
func (app *App) formatMessage(template, logFile, timestamp, level string, content string) string {
	result := template

	// 收集带 “.” 替换为 “_”
	var replaceKeys = make(map[string]string)
	var replacementKeys = make([]string, 0)
	for _, v := range app.config.ContentMaps {
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

// monitorLogFile 监控单个日志文件
func (app *App) monitorLogFile(ctx context.Context, logFile string, startLine int, config AppConfig, levelRe, logRe *regexp.Regexp, offsetStore *OffsetStore, sigManager *SignatureManager, logger *logrus.Logger) {
	// 确定起始偏移量
	var offset = offsetStore.Get(logFile)
	if startLine > 0 {
		// 根据起始行数计算偏移量
		lineOffset, err := app.getOffsetByLine(logFile, startLine)
		if err != nil {
			logger.Errorf("The starting line offset of %s cannot be calculated: %v", logFile, err)
			return
		}
		if lineOffset > offset {
			offset = lineOffset
		}
	}

	t, err := tail.TailFile(logFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Location:  &tail.SeekInfo{Offset: offset, Whence: io.SeekStart},
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
			if err := offsetStore.Save(); err != nil {
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

			message := app.formatMessage(config.MessageFormat, logFile, timestamp, level, jsonPart)
			if err := app.sendToLarkWithRetry(config.WebhookURL, message, sigManager); err != nil {
				logger.Errorf("Failed to send to Lark for %s: %v", logFile, err)
			} else {
				logger.Infof("Sent to Lark for %s", logFile)
			}
		}
	}
}

// sendToLarkWithRetry 发送消息到 Lark Webhook，支持重试
func (app *App) sendToLarkWithRetry(webhookURL, message string, sigManager *SignatureManager) error {
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

// getOffsetByLine 计算指定行数的偏移量
func (app *App) getOffsetByLine(file string, startLine int) (int64, error) {
	if startLine <= 0 {
		return 0, nil
	}

	f, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("file [%s] cannot be opened: %v", file, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var offset int64
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		lineBytes := scanner.Bytes()
		offset += int64(len(lineBytes)) + 1 // 加上换行符
		if lineCount == startLine {
			return offset, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read the file %s: %v", file, err)
	}

	return offset, nil
}
