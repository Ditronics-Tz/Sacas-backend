package logger

import (
	"fmt"
	"log"
	"os"
	"time"

	"go_boilerplate/internal/config"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l LogLevel) String() string {
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

type Logger struct {
	level  LogLevel
	logger *log.Logger
}

var defaultLogger *Logger

func init() {
	defaultLogger = New()
}

func New() *Logger {
	return &Logger{
		level:  INFO,
		logger: log.New(os.Stdout, "", 0),
	}
}

func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	
	logEntry := fmt.Sprintf("[%s] %s: %s", timestamp, level.String(), message)
	
	if level == FATAL {
		l.logger.Fatal(logEntry)
	} else {
		l.logger.Println(logEntry)
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
}

// Global logging functions
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

func SetLevel(level LogLevel) {
	defaultLogger.SetLevel(level)
}

// InitLogger initializes the logger with environment configuration
func InitLogger() {
	logLevel := config.GetEnv("LOG_LEVEL", "info")
	
	switch logLevel {
	case "debug":
		SetLevel(DEBUG)
	case "info":
		SetLevel(INFO)
	case "warn":
		SetLevel(WARN)
	case "error":
		SetLevel(ERROR)
	default:
		SetLevel(INFO)
	}
	
	Info("Logger initialized with level: %s", logLevel)
}