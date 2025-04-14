package logger

import (
	"fmt"
	"os"
	"testing"

	"github.com/veiloq/emberkit/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// initLogger initializes the zap logger (zaptest or default).
// It returns the logger, a boolean indicating if it's a test logger, and an error.
func InitLogger(t *testing.T, options *config.Settings) (*zap.Logger, bool, error) {
	var logger *zap.Logger
	var isTestLogger bool

	if t != nil {
		// Use zaptest logger
		zaptestOpts := []zaptest.LoggerOption{}
		if options.ZapTestLevel() != nil {
			zaptestOpts = append(zaptestOpts, zaptest.Level(*options.ZapTestLevel()))
		}
		logger = zaptest.NewLogger(t, zaptestOpts...)
		isTestLogger = true
		if len(options.ZapOptions()) > 0 {
			logger = logger.WithOptions(options.ZapOptions()...)
		}
		logger.Debug("Initialized zaptest logger")
	} else {
		// Fallback to default zap logger
		var err error
		// Ensure the log directory exists
		logDir := ".emberkit"
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, false, fmt.Errorf("failed to create log directory %s: %w", logDir, err)
		}
		logFilePath := fmt.Sprintf("%s/LOG", logDir)

		devConfig := zap.NewDevelopmentConfig()
		// Log to both stdout and the file
		devConfig.OutputPaths = []string{"stdout", logFilePath}
		devConfig.ErrorOutputPaths = []string{"stderr", logFilePath}

		// Note: AddCallerSkip(1) might need adjustment depending on call stack depth after refactor.
		// Keeping it for now, but might need review later.
		zapBaseOpts := []zap.Option{zap.AddCallerSkip(1)}
		if options != nil {
			zapBaseOpts = append(zapBaseOpts, options.ZapOptions()...)
		}
		logger, err = devConfig.Build(zapBaseOpts...)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create default zap logger: %w", err)
		}
		isTestLogger = false
		logger.Debug("Initialized default zap development logger (no *testing.T provided)")
	}
	return logger, isTestLogger, nil
}
