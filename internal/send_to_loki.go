package internal

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/grafana/loki-client-go/loki"
	"github.com/grafana/loki-client-go/pkg/labelutil"
	"github.com/grafana/loki-client-go/pkg/urlutil"
	"sync"
	"time"
)

// LogEntry 定义日志条目结构
type LogEntry struct {
	Level   string `json:"level"`
	Ts      string `json:"ts"`
	Caller  string `json:"caller"`
	Module  string `json:"module"`
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
	Msg     string `json:"msg"`
}

// SendToLokiManager 用于上报到 Loki
type SendToLokiManager struct {
	client *loki.Client
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSendToLokiManager 创建 SendToLokiManager 实例
func NewSendToLokiManager(lokiURL string) (*SendToLokiManager, error) {
	var u urlutil.URLValue
	if err := u.Set(lokiURL); err != nil {
		return nil, err
	}

	config := loki.Config{
		URL:       u,
		BatchWait: 1 * time.Second,
		BatchSize: 1000,
		Timeout:   10 * time.Second,
	}
	client, err := loki.New(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &SendToLokiManager{
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Stop 停止 LogHelper
func (h *SendToLokiManager) stop() {
	h.client.Stop()
}

// Send 上报到 Loki
func (h *SendToLokiManager) sendWithRetry(content, timeStr string) error {
	// 上报到 Loki
	var ls labelutil.LabelSet
	err := ls.Set("app=my-service")
	if err != nil {
		return err
	}

	ts, _ := time.Parse("2006-01-02T15:04:05.000Z0700", timeStr)

	operation := func() error {
		if err := h.client.Handle(ls.LabelSet, ts, content); err != nil {
			return fmt.Errorf("failed to send log to Loki: %v", err)
		}
		return nil
	}

	// 配置指数退避重试
	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 30 * time.Second
	return backoff.Retry(operation, backoff.WithContext(bo, context.Background()))
}
