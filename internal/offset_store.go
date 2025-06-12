package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
)

// OffsetStore 存储每个日志文件的偏移量
type OffsetStore struct {
	Offsets map[string]int64 `json:"offsets"`
	file    string
	mu      sync.Mutex
}

// NewOffsetStore 初始化偏移量存储
func NewOffsetStore(file string) *OffsetStore {
	return &OffsetStore{
		Offsets: make(map[string]int64),
		file:    file,
	}
}

// Save 保存偏移量到文件
func (o *OffsetStore) Save() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	data, err := json.Marshal(o.Offsets)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(o.file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	var buf bytes.Buffer
	buf.Write(data)
	_, err = io.Copy(f, &buf)
	if err != nil {
		return err
	}
	return f.Sync()
}

// Load 从文件中加载偏移量
func (o *OffsetStore) Load() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	f, err := os.OpenFile(o.file, os.O_RDONLY, 0644)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, &o.Offsets)
}

// checkFile 检查文件是否存在
func (o *OffsetStore) checkFileExist() error {
	for file, _ := range o.Offsets {
		if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
			o.Remove(file)
		}
	}
	return o.Save()
}

// Set 更新偏移量
func (o *OffsetStore) Set(file string, offset int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Offsets[file] = offset
}

// Get 获取偏移量
func (o *OffsetStore) Get(file string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.Offsets[file]
}

// Remove 移除偏移量
func (o *OffsetStore) Remove(file string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.Offsets, file)
}
