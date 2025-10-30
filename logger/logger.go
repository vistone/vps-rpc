package logger

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Logger 日志接口
type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger
}

// LogLevel 日志级别
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogFormat 日志格式
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// logrusLogger logrus实现的Logger
type logrusLogger struct {
	logger *logrus.Logger
}

// NewLogger 创建新的日志器
// 参数:
//
//	level: 日志级别
//	format: 日志格式
//	output: 输出位置（nil表示标准输出）
//
// 返回值:
//
//	Logger: 日志器实例
func NewLogger(level LogLevel, format LogFormat, output io.Writer) Logger {
	logger := logrus.New()

	// 设置日志级别
	switch level {
	case LogLevelDebug:
		logger.SetLevel(logrus.DebugLevel)
	case LogLevelInfo:
		logger.SetLevel(logrus.InfoLevel)
	case LogLevelWarn:
		logger.SetLevel(logrus.WarnLevel)
	case LogLevelError:
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}

	// 设置日志格式
	switch format {
	case LogFormatJSON:
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		})
	case LogFormatText:
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	default:
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	}

	// 设置输出位置
	if output != nil {
		logger.SetOutput(output)
	} else {
		logger.SetOutput(os.Stdout)
	}

	return &logrusLogger{logger: logger}
}

// NewLoggerFromConfig 从配置创建日志器
func NewLoggerFromConfig(level string, format string) Logger {
	logLevel := LogLevel(level)
	if logLevel == "" {
		logLevel = LogLevelInfo
	}

	logFormat := LogFormat(format)
	if logFormat == "" {
		logFormat = LogFormatText
	}

	return NewLogger(logLevel, logFormat, nil)
}

// Debug 输出调试日志
func (l *logrusLogger) Debug(args ...interface{}) {
	l.logger.Debug(args...)
}

// Debugf 格式化输出调试日志
func (l *logrusLogger) Debugf(format string, args ...interface{}) {
	l.logger.Debugf(format, args...)
}

// Info 输出信息日志
func (l *logrusLogger) Info(args ...interface{}) {
	l.logger.Info(args...)
}

// Infof 格式化输出信息日志
func (l *logrusLogger) Infof(format string, args ...interface{}) {
	l.logger.Infof(format, args...)
}

// Warn 输出警告日志
func (l *logrusLogger) Warn(args ...interface{}) {
	l.logger.Warn(args...)
}

// Warnf 格式化输出警告日志
func (l *logrusLogger) Warnf(format string, args ...interface{}) {
	l.logger.Warnf(format, args...)
}

// Error 输出错误日志
func (l *logrusLogger) Error(args ...interface{}) {
	l.logger.Error(args...)
}

// Errorf 格式化输出错误日志
func (l *logrusLogger) Errorf(format string, args ...interface{}) {
	l.logger.Errorf(format, args...)
}

// Fatal 输出致命错误日志并退出
func (l *logrusLogger) Fatal(args ...interface{}) {
	l.logger.Fatal(args...)
}

// Fatalf 格式化输出致命错误日志并退出
func (l *logrusLogger) Fatalf(format string, args ...interface{}) {
	l.logger.Fatalf(format, args...)
}

// WithField 添加字段
func (l *logrusLogger) WithField(key string, value interface{}) Logger {
	return &logrusLogger{logger: l.logger.WithField(key, value).Logger}
}

// WithFields 添加多个字段
func (l *logrusLogger) WithFields(fields map[string]interface{}) Logger {
	return &logrusLogger{logger: l.logger.WithFields(logrus.Fields(fields)).Logger}
}

// LogEntry 日志条目（用于日志查询）
type LogEntry struct {
	Timestamp int64                  `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// LogBuffer 日志缓冲区（用于存储日志供查询）
type LogBuffer struct {
	entries []LogEntry
	maxSize int
	mutex   sync.RWMutex
}

var (
	globalLogBuffer *LogBuffer
	logBufferOnce   sync.Once
)

// GetLogBuffer 获取全局日志缓冲区
func GetLogBuffer(maxSize int) *LogBuffer {
	logBufferOnce.Do(func() {
		globalLogBuffer = &LogBuffer{
			entries: make([]LogEntry, 0, maxSize),
			maxSize: maxSize,
		}
	})
	return globalLogBuffer
}

// AddEntry 添加日志条目
func (b *LogBuffer) AddEntry(level string, message string, fields map[string]interface{}) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now().Unix(),
		Level:     level,
		Message:   message,
		Fields:    fields,
	}

	// 如果缓冲区已满，删除最旧的条目
	if len(b.entries) >= b.maxSize {
		b.entries = b.entries[1:]
	}

	b.entries = append(b.entries, entry)
}

// GetEntries 获取日志条目
func (b *LogBuffer) GetEntries(level string, maxLines int) []LogEntry {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	var result []LogEntry
	count := 0

	// 从后往前遍历（最新的在前）
	for i := len(b.entries) - 1; i >= 0 && count < maxLines; i-- {
		entry := b.entries[i]
		if level == "" || entry.Level == level {
			result = append([]LogEntry{entry}, result...)
			count++
		}
	}

	return result
}

// Clear 清空日志缓冲区
func (b *LogBuffer) Clear() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.entries = b.entries[:0]
}

// Size 获取缓冲区大小
func (b *LogBuffer) Size() int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return len(b.entries)
}

