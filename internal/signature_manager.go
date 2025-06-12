package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// SignatureManager 管理 Webhook 签名
type SignatureManager struct {
	Secret     string
	Timestamp  int64
	Signature  string
	Expiration int64
	mu         sync.Mutex
}

// NewSignatureManager 初始化签名管理
func NewSignatureManager(secret string) *SignatureManager {
	return &SignatureManager{
		Secret: secret,
	}
}

// GetSignature 获取当前签名，如果过期则重新生成
func (s *SignatureManager) GetSignature() (int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentTime := time.Now().Unix()
	if s.Signature != "" && currentTime < s.Expiration {
		return s.Timestamp, s.Signature, nil
	}

	// 生成新签名
	timestamp := currentTime
	signature, err := s.GenSign(s.Secret, timestamp)
	if err != nil {
		return 0, "", err
	}

	// 更新签名和过期时间
	s.Timestamp = timestamp
	s.Signature = signature
	s.Expiration = timestamp + 3000

	return s.Timestamp, s.Signature, nil
}

// GenSign 生成 Webhook 签名, URL: https://open.larksuite.com/document/ukTMukTMukTM/ucTM5YjL3ETO24yNxkjN
func (s *SignatureManager) GenSign(secret string, timestamp int64) (string, error) {
	// timestamp + key 做sha256, 再进行base64 encode
	stringToSign := fmt.Sprintf("%v", timestamp) + "\n" + secret
	var data []byte
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, err := h.Write(data)
	if err != nil {
		return "", err
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return signature, nil
}
