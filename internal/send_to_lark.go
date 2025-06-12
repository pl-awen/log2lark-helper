package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"net/http"
	"strings"
	"time"
)

type SendToLarkManager struct {
	webhookURL string
	sigManager *SignatureManager
}

// NewSendToLarkManager ..
func NewSendToLarkManager(webhookURL string, sigManager *SignatureManager) *SendToLarkManager {
	return &SendToLarkManager{
		webhookURL: webhookURL,
		sigManager: sigManager,
	}
}

// sendWithRetry 发送消息到 Lark Webhook，支持重试
func (sm *SendToLarkManager) sendWithRetry(message string) error {
	msg := LarkMessage{
		MsgType: "text",
	}
	msg.Content.Text = message

	// 获取签名（如果有密钥）
	timestamp, signature, err := sm.sigManager.GetSignature()
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
		req, err := http.NewRequest("POST", sm.webhookURL, strings.NewReader(string(payload)))
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
