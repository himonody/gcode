package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
}

type Field struct {
	Key   string
	Value interface{}
}

func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func ErrorField(err error) Field {
	return Field{Key: "error", Value: err}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

type defaultLogger struct {
	level  Level
	logger *log.Logger
	fields []Field
	mu     sync.Mutex
}

func NewLogger(level Level) Logger {
	return &defaultLogger{
		level:  level,
		logger: log.New(os.Stdout, "", 0),
		fields: make([]Field, 0),
	}
}

func (l *defaultLogger) log(level Level, msg string, fields ...Field) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	allFields := append(l.fields, fields...)
	fieldStr := ""
	if len(allFields) > 0 {
		fieldStr = " "
		for i, f := range allFields {
			if i > 0 {
				fieldStr += " "
			}
			fieldStr += fmt.Sprintf("%s=%v", f.Key, f.Value)
		}
	}

	l.logger.Printf("[%s] %s: %s%s", timestamp, level.String(), msg, fieldStr)

	if level == FATAL {
		os.Exit(1)
	}
}

func (l *defaultLogger) Debug(msg string, fields ...Field) {
	l.log(DEBUG, msg, fields...)
}

func (l *defaultLogger) Info(msg string, fields ...Field) {
	l.log(INFO, msg, fields...)
}

func (l *defaultLogger) Warn(msg string, fields ...Field) {
	l.log(WARN, msg, fields...)
}

func (l *defaultLogger) Error(msg string, fields ...Field) {
	l.log(ERROR, msg, fields...)
}

func (l *defaultLogger) Fatal(msg string, fields ...Field) {
	l.log(FATAL, msg, fields...)
}

func (l *defaultLogger) With(fields ...Field) Logger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)

	return &defaultLogger{
		level:  l.level,
		logger: l.logger,
		fields: newFields,
	}
}

var globalLogger Logger = NewLogger(INFO)

func SetGlobalLogger(logger Logger) {
	globalLogger = logger
}

func Debug(msg string, fields ...Field) {
	globalLogger.Debug(msg, fields...)
}

func Info(msg string, fields ...Field) {
	globalLogger.Info(msg, fields...)
}

func Warn(msg string, fields ...Field) {
	globalLogger.Warn(msg, fields...)
}

func Error(msg string, fields ...Field) {
	globalLogger.Error(msg, fields...)
}

func Fatal(msg string, fields ...Field) {
	globalLogger.Fatal(msg, fields...)
}
