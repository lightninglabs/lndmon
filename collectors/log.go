package collectors

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/btcsuite/btclog/v2"
	"github.com/lightningnetwork/lnd/build"
)

var (
	// Logger for lndmon's main process.
	Logger btclog.Logger

	// htlcLogger is a logger for lndmon's htlc collector.
	htlcLogger btclog.Logger

	// paymentLogger is a logger for lndmon's payments monitor.
	paymentLogger btclog.Logger

	// watchtowerLogger is a logger for lndmon's watchtower client.
	watchtowerLogger btclog.Logger

	noOpShutdownFunc = func() {}
)

// initLogRotator initializes the logging rotator to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotator variables are used.
func initLogRotator(logFile string, maxLogFileSize, maxLogFiles int) error {
	logDir, _ := filepath.Split(logFile)
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Setup the rotating log writer.
	logRotator := build.NewRotatingLogWriter()
	logCfg := build.DefaultLogConfig()
	logCfg.File.MaxLogFileSize = maxLogFileSize
	logCfg.File.MaxLogFiles = maxLogFiles
	logCfg.File.Compressor = build.Gzip // Optional: or build.Zstd

	if err := logRotator.InitLogRotator(logCfg.File, logFile); err != nil {
		return fmt.Errorf("failed to init log rotator: %w", err)
	}

	// Create the log handlers (console + rotating file).
	logHandlers := build.NewDefaultLogHandlers(logCfg, logRotator)

	// Create the subsystem logger manager.
	logManager := build.NewSubLoggerManager(logHandlers...)

	// Create subsystem loggers.
	Logger = logManager.GenSubLogger("LNDMON", noOpShutdownFunc)
	htlcLogger = logManager.GenSubLogger("HTLC", noOpShutdownFunc)
	paymentLogger = logManager.GenSubLogger("PMNT", noOpShutdownFunc)
	watchtowerLogger = logManager.GenSubLogger("WTCL", noOpShutdownFunc)

	// Set log level.
	// TODO: consider making this configurable.
	logManager.SetLogLevels("info")

	return nil
}
