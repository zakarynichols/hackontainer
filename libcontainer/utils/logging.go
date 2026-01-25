package utils

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

type LoggingConfig struct {
	Debug     bool
	LogFile   string
	LogFormat string
}

func SetupLogging(config *LoggingConfig) error {
	if config.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetReportCaller(true)

		_, file, _, _ := runtime.Caller(0)
		prefix := strings.TrimSuffix(file, "/logging/logging.go") + "/"
		logrus.SetFormatter(&logrus.TextFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				function := strings.TrimPrefix(f.Function, prefix) + "()"
				fileLine := strings.TrimPrefix(f.File, prefix) + ":" + strconv.Itoa(f.Line)
				return function, fileLine
			},
		})
	}

	switch config.LogFormat {
	case "":
	case "text":
	case "json":
		logrus.SetFormatter(new(logrus.JSONFormatter))
	default:
		return errors.New("invalid log-format: " + config.LogFormat)
	}

	if config.LogFile != "" {
		f, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0o644)
		if err != nil {
			return err
		}
		logrus.SetOutput(f)
	}

	return nil
}

func Fatal(args ...interface{}) {
	logrus.Fatal(args...)
}

func Error(args ...interface{}) {
	logrus.Error(args...)
}

func Warn(args ...interface{}) {
	logrus.Warn(args...)
}

func Info(args ...interface{}) {
	logrus.Info(args...)
}

func Debug(args ...interface{}) {
	logrus.Debug(args...)
}

func Fatalf(format string, args ...interface{}) {
	logrus.Fatalf(format, args...)
}

func Errorf(format string, args ...interface{}) {
	logrus.Errorf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	logrus.Warnf(format, args...)
}

func Infof(format string, args ...interface{}) {
	logrus.Infof(format, args...)
}

func Debugf(format string, args ...interface{}) {
	logrus.Debugf(format, args...)
}

func SecureJoin(root, path string) (string, error) {
	if root == "" {
		return "", nil
	}
	return filepath.Join(root, path), nil
}

func CleanPath(path string) string {
	return filepath.Clean(path)
}
