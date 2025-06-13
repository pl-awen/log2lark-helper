package internal

import (
	"context"
	"github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"regexp"
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
	CacheTimeSecond        int
	CacheContentIndex      string
	MatchRule              string
	OffsetFile             string
	LogRegex               string
	IncludeRegex           string
	ExcludeRegex           string
	JsonPartContentIndex   string
	LevelFieldIndex        string
	ContentMaps            []string
	MessageFormat          string
	EnableRAWLogFormat     bool
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
		logrus.Fatalf("Invalid level rule: %v", err)
	}

	logRe, err := regexp.Compile(app.config.LogRegex)
	if err != nil {
		logrus.Fatalf("Invalid log regex: %v", err)
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

	// 缓存
	var memoryCache *MemoryCache
	if app.config.CacheTimeSecond > 0 {
		second := time.Duration(app.config.CacheTimeSecond) * time.Second
		memoryCache = NewMemoryCache("LOG-CACHE", second, time.Second*5)
		defer memoryCache.Close()
	}

	// 创建监听管理
	mparms := MonitorParams{
		LevelRe:                levelRe,
		LogRe:                  logRe,
		WatchDirFileSuffixList: app.config.WatchDirFileSuffixList,
		JsonPartContentIndex:   app.config.JsonPartContentIndex,
		LevelFieldIndex:        app.config.LevelFieldIndex,
		ContentMaps:            app.config.ContentMaps,
		MessageFormat:          app.config.MessageFormat,
		CacheContentIndex:      app.config.CacheContentIndex,
		EnableRAWLogFormat:     app.config.EnableRAWLogFormat,
	}

	if app.config.ExcludeRegex != "" {
		excludeRe, err := regexp.Compile(app.config.ExcludeRegex)
		if err != nil {
			logrus.Fatalf("Invalid exclude regex: %v", err)
		}
		mparms.ExcludeRe = excludeRe
	}

	if app.config.IncludeRegex != "" {
		includeRe, err := regexp.Compile(app.config.IncludeRegex)
		if err != nil {
			logrus.Fatalf("Invalid include regex: %v", err)
		}
		mparms.IncludeRe = includeRe
	}

	monitorManager := NewMonitorManager(mparms, sendLarkManager, offsetStore, memoryCache, logger)

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
