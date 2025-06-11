package internal

// LarkMessage 定义 Webhook 的消息结构
type LarkMessage struct {
	Timestamp string `json:"timestamp,omitempty"`
	Sign      string `json:"sign,omitempty"`
	MsgType   string `json:"msg_type"`
	Content   struct {
		Text string `json:"text"`
	} `json:"content"`
}

// LogEntry 定义日志的 JSON 部分结构
type LogEntry struct {
	Caller    string `json:"caller"`
	ServiceID string `json:"service.id"`
	Msg       string `json:"msg"`
	TraceID   string `json:"trace.id"`
	SpanID    string `json:"span.id"`
}
