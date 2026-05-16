package logging

import (
	"io"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

var (
	mu         sync.Mutex
	logger     = log.New(os.Stdout, "", log.LstdFlags)
	logFile    *os.File
	logDir     string
	currentDay string
	toFile     bool
)

// EnableFile turns on dual logging (stdout + dated log file in dir).
// File rotation happens automatically on day change.
func EnableFile(dir string) error {
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	logDir = dir
	toFile = true
	return openTodayLocked()
}

func openTodayLocked() error {
	day := time.Now().Format("2006-01-02")
	if logFile != nil && day == currentDay {
		return nil
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	f, err := os.OpenFile(path.Join(logDir, day+".log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o664)
	if err != nil {
		return err
	}
	logFile = f
	currentDay = day
	logger.SetOutput(io.MultiWriter(os.Stdout, f))
	return nil
}

func rotateIfNeeded() {
	if !toFile {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	_ = openTodayLocked()
}

func Info(args ...interface{}) {
	rotateIfNeeded()
	logger.SetPrefix("INFO: ")
	logger.Println(args...)
}

func Warn(args ...interface{}) {
	rotateIfNeeded()
	logger.SetPrefix("WARN: ")
	logger.Println(args...)
}

func Fatal(args ...interface{}) {
	rotateIfNeeded()
	logger.SetPrefix("FATAL: ")
	logger.Println(args...)
	if logFile != nil {
		_ = logFile.Close()
	}
	os.Exit(1)
}
