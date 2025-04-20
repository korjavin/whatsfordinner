package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Logger is a wrapper around the standard library logger
type Logger struct {
	*log.Logger
	channelID string
}

// New creates a new logger with the given channel ID
func New(channelID string) *Logger {
	return &Logger{
		Logger:    log.New(os.Stdout, "", 0),
		channelID: channelID,
	}
}

// formatMessage formats a log message with timestamp and channel ID
func (l *Logger) formatMessage(level, format string, v ...interface{}) string {
	timestamp := time.Now().Format(time.RFC3339)
	message := fmt.Sprintf(format, v...)
	
	if l.channelID != "" {
		return fmt.Sprintf("[%s] [%s] [Channel: %s] %s", timestamp, level, l.channelID, message)
	}
	
	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, message)
}

// Info logs an info message
func (l *Logger) Info(format string, v ...interface{}) {
	l.Logger.Println(l.formatMessage("INFO", format, v...))
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	l.Logger.Println(l.formatMessage("ERROR", format, v...))
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	l.Logger.Println(l.formatMessage("DEBUG", format, v...))
}

// Warn logs a warning message
func (l *Logger) Warn(format string, v ...interface{}) {
	l.Logger.Println(l.formatMessage("WARN", format, v...))
}

// Global logger instance for application-wide logging
var Global = New("")

// SetGlobal sets the global logger
func SetGlobal(logger *Logger) {
	Global = logger
}
