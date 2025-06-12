package internal

import (
	"bufio"
	"context"
	"crypto/md5"
	"fmt"
	"github.com/hpcloud/tail"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"gopkg.in/fsnotify.v1"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// MonitorManager 管理正在监控的文件
type MonitorManager struct {
	monitors          sync.Map // map[string]context.CancelFunc
	params            MonitorParams
	sendToLarkManager *SendToLarkManager
	memoryCache       *MemoryCache
	logger            *logrus.Logger
	offsetStore       *OffsetStore
}

type MonitorParams struct {
	WatchDirFileSuffixList []string
	LevelRe                *regexp.Regexp
	LogRe                  *regexp.Regexp
	JsonPartContentIndex   string
	LevelFieldIndex        string
	ContentMaps            []string
	MessageFormat          string
}

// NewMonitorManager ..
func NewMonitorManager(params MonitorParams, sendToLarkManager *SendToLarkManager, offsetStore *OffsetStore, memoryCache *MemoryCache, logger *logrus.Logger) *MonitorManager {
	return &MonitorManager{
		params:            params,
		sendToLarkManager: sendToLarkManager,
		offsetStore:       offsetStore,
		memoryCache:       memoryCache,
		logger:            logger,
	}
}

// StartFileMonitor 启动文件监控
func (mm *MonitorManager) StartFileMonitor(ctx context.Context, logFile string, lastStartLine int) {
	// 检查是否已监控
	if _, loaded := mm.monitors.LoadOrStore(logFile, nil); loaded {
		mm.logger.Infof("File %s has been monitored. Skip it", logFile)
		return
	}

	// 创建子上下文
	fileCtx, cancel := context.WithCancel(ctx)
	mm.monitors.Store(logFile, cancel)

	mm.logger.Infof("Start monitoring the new file: %s (starting line: %d)", logFile, lastStartLine)

	defer func() {
		mm.monitors.Delete(logFile)
		mm.logger.Infof("Stop monitoring file: %s", logFile)
	}()
	mm.monitorLogFile(fileCtx, logFile, lastStartLine)
}

// AsyncStartFileMonitor 启动文件监控
func (mm *MonitorManager) AsyncStartFileMonitor(ctx context.Context, wg *sync.WaitGroup, logFile string, lastStartLine int) {
	wg.Add(1)
	go func(c context.Context, lf string, lsl int) {
		defer wg.Done()
		mm.StartFileMonitor(c, lf, lsl)
	}(ctx, logFile, lastStartLine)
}

// StartWatchDirectory 监听单个目录
func (mm *MonitorManager) StartWatchDirectory(ctx context.Context, dir string, wg *sync.WaitGroup) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		mm.logger.Fatalf("Directory listener cannot be created: %v", err)
	}
	defer watcher.Close()

	err = watcher.Add(dir)
	if err != nil {
		mm.logger.Fatalf("The directory %s: %v cannot be monitored", dir, err)
	}

	// 监听初始化文件
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		for _, suffix := range mm.params.WatchDirFileSuffixList {
			if !strings.HasSuffix(path, suffix) {
				return nil
			}
			mm.AsyncStartFileMonitor(ctx, wg, path, 0)
		}

		return nil
	})
	if err != nil {
		mm.logger.Fatalf("The files under this directory %s: %v cannot be monitored", dir, err)
	}

	mm.logger.Infof("Start listening to the directory: %s", dir)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				mm.logger.Warn("The directory listening channel is closed")
				return
			}

			var isContinue bool
			for _, suffix := range mm.params.WatchDirFileSuffixList {
				if !strings.HasSuffix(event.Name, suffix) {
					isContinue = true
					break
				}
			}

			if isContinue {
				continue
			}

			switch {
			case event.Op&fsnotify.Create == fsnotify.Create:
				mm.logger.Infof("A new file was detected: %s", event.Name)
				mm.AsyncStartFileMonitor(ctx, wg, event.Name, 0)
			//case event.Op&fsnotify.Write == fsnotify.Write:
			//	mm.logger.Infof("File modification detected: %s", event.Name)
			//	mm.AsyncStartFileMonitor(ctx, wg, event.Name, 0)
			case event.Op&fsnotify.Remove == fsnotify.Remove:
				mm.logger.Infof("File deletion detected: %s", event.Name)
				mm.StopMonitor(event.Name)
				mm.offsetStore.Remove(event.Name)
				if err = mm.offsetStore.Save(); err != nil {
					mm.logger.Errorf("Offset: %v cannot be saved", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				mm.logger.Warn("The directory error channel is closed")
				return
			}
			mm.logger.Errorf("Directory listening error: %v", err)
		}
	}
}

// StopMonitor 停止文件监控
func (mm *MonitorManager) StopMonitor(logFile string) {
	if cancel, ok := mm.monitors.Load(logFile); ok {
		if cancelFunc, ok := cancel.(context.CancelFunc); ok {
			cancelFunc()
		}
		mm.monitors.Delete(logFile)
		mm.logger.Infof("Removed monitoring: %s", logFile)
	}
}

// monitorLogFile 监控单个日志文件
func (mm *MonitorManager) monitorLogFile(ctx context.Context, logFile string, lastStartLine int) {
	// 确定起始偏移量
	var offset = mm.offsetStore.Get(logFile)
	var err error
	if lastStartLine > 0 {
		// 根据末尾起始行数计算偏移量
		offset, err = mm.getLastNLinesOffset(logFile, lastStartLine)
		if err != nil {
			mm.logger.Errorf("The last starting line offset of %s cannot be calculated: %v", logFile, err)
			return
		}
	} else if offset == 0 {
		offset, err = mm.getLastNLinesOffset(logFile, lastStartLine)
		if err != nil {
			mm.logger.Errorf("The last starting line offset of %s cannot be calculated: %v", logFile, err)
			return
		}
	}

	t, err := tail.TailFile(logFile, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Location:  &tail.SeekInfo{Offset: offset, Whence: io.SeekStart},
	})
	if err != nil {
		mm.logger.Errorf("Failed to tail file %s: %v", logFile, err)
		return
	}
	defer t.Cleanup()

	mm.logger.Infof("Monitoring file: %s", logFile)

	for {
		select {
		case <-ctx.Done():
			// 获取当前偏移量
			offset, err = t.Tell()
			if err != nil {
				mm.logger.Errorf("Failed to get offset for %s: %v", logFile, err)
			} else {
				mm.offsetStore.Set(logFile, offset)
			}
			if err = mm.offsetStore.Save(); err != nil {
				mm.logger.Errorf("Failed to save offsets for %s: %v", logFile, err)
			}
			return
		case line, ok := <-t.Lines:
			if !ok {
				mm.logger.Warnf("Tail channel closed for %s", logFile)
				return
			}
			if line.Err != nil {
				mm.logger.Errorf("Error reading line from %s: %v", logFile, line.Err)
				continue
			}

			// 更新偏移量
			offset, err = t.Tell()
			if err != nil {
				mm.logger.Errorf("Failed to get offset for %s: %v", logFile, err)
				continue
			}
			mm.offsetStore.Set(logFile, offset)

			// 解析日志行
			matches := mm.params.LogRe.FindStringSubmatch(line.Text)

			// 解析索引
			var jsonPart string
			jsonPartIndexStr := strings.ReplaceAll(mm.params.JsonPartContentIndex, "#", "")
			jsonPartIndex, err := strconv.Atoi(jsonPartIndexStr)
			if err != nil {
				mm.logger.Errorf("Failed to parse json part index from %s: %v", logFile, err)
				continue
			}

			if len(matches) <= jsonPartIndex {
				mm.logger.Warn("Found json part index %d in %s", jsonPartIndex, matches)
				continue
			} else {
				jsonPart = matches[jsonPartIndex]
			}

			var level string
			if strings.HasPrefix(mm.params.LevelFieldIndex, "#") {
				levelIndexStr := strings.ReplaceAll(mm.params.LevelFieldIndex, "#", "")
				levelIndex, err := strconv.Atoi(levelIndexStr)
				if err != nil {
					mm.logger.Warn("Failed to parse level index from %s: %v", logFile, err)
					continue
				}

				if len(matches) <= levelIndex {
					mm.logger.Warn("Found level index %d in %s", levelIndex, matches)
					continue
				} else {
					level = matches[levelIndex]
				}
			} else {
				level = gjson.Get(jsonPart, mm.params.LevelFieldIndex).String()
			}

			// 匹配日志级别
			if !mm.params.LevelRe.MatchString(level) {
				continue
			}

			// 使用缓存限制频率
			if mm.memoryCache != nil {
				key := mm.computeMD5(jsonPart)
				content, err := mm.memoryCache.GetCache(ctx, key)
				if err != nil {
					mm.logger.Warnf("Failed to get content for %s: %v", key, err)
				}

				if content != "" {
					continue
				}

				if err = mm.memoryCache.SetCache(ctx, key, jsonPart); err != nil {
					mm.logger.Warnf("Failed to set content for %s: %v", key, err)
					continue
				}
			}

			message := formatMessage(matches, mm.params.MessageFormat, logFile, level, jsonPart, mm.params.ContentMaps)
			if err = mm.sendToLarkManager.sendWithRetry(message); err != nil {
				mm.logger.Errorf("Failed to send to Lark for %s: %v", logFile, err)
			} else {
				mm.logger.Infof("Sent to Lark for %s", logFile)
			}
		}
	}
}

// computeMD5 计算 MD5
func (mm *MonitorManager) computeMD5(text string) string {
	hash := md5.Sum([]byte(text))
	return fmt.Sprintf("%x", hash)
}

// getOffsetByLine 计算指定行数的偏移量
func (mm *MonitorManager) getOffsetByLine(file string, startLine int) (int64, error) {
	if startLine == 0 {
		return 0, nil
	}

	f, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("file [%s] cannot be opened: %v", file, err)
	}
	defer f.Close()

	if startLine == -1 {

	}

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

// getLastNLinesOffset 计算文件最后 n 行的起始偏移量
func (mm *MonitorManager) getLastNLinesOffset(filePath string, n int) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("file opening failed: %v", err)
	}
	defer file.Close()

	// 获取文件大小
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to obtain file information: %v", err)
	}
	fileSize := fileInfo.Size()

	// 如果文件为空，直接返回
	if fileSize == 0 {
		return 0, nil
	}

	// 从文件末尾开始逆向读取
	lineCount := 0
	// 从最后一个字节开始
	offset := fileSize - 1

	// 存储当前字节，假设文件末尾是最后一行开始
	var lastLineStart = fileSize

	for offset >= 0 {
		// 定位到当前偏移量
		_, err := file.Seek(offset, 0)
		if err != nil {
			return 0, fmt.Errorf("seek 失败: %v", err)
		}

		// 读取一个字节
		b := make([]byte, 1)
		_, err = file.Read(b)
		if err != nil {
			return 0, fmt.Errorf("读取字节失败: %v", err)
		}

		// 检查是否为换行符
		if b[0] == '\n' {
			lineCount++
			if lineCount == n {
				// 找到第 n 行的起始偏移量（换行符后的下一个字节）
				lastLineStart = offset + 1
				break
			}
		}

		// 向前移动
		offset--
	}

	// 如果行数不足 n，返回文件开头
	if lineCount < n {
		return 0, nil
	}

	return lastLineStart, nil
}
