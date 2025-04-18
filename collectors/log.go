package collectors

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/btcsuite/btclog"
	"github.com/jrick/logrotate/rotator"
	"github.com/lightningnetwork/lnd/build"
)

var (
	logWriter  = &build.LogWriter{}
	backendLog = btclog.NewBackend(logWriter)

	// Logger for lndmon's main process.
	Logger = backendLog.Logger("LNDMON")

	// htlcLogger is a logger for lndmon's htlc collector.
	htlcLogger = build.NewSubLogger("HTLC", backendLog.Logger)

	// paymentLogger is a logger for lndmon's payments monitor.
	paymentLogger = build.NewSubLogger("PMNT", backendLog.Logger)
)

// initLogRotator initializes the logging rotator to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotator variables are used.
func initLogRotator(logFile string, maxLogFileSize int, maxLogFiles int) error {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	r, err := rotator.New(
		logFile, int64(maxLogFileSize*1024), false, maxLogFiles,
	)
	if err != nil {
		return fmt.Errorf("failed to create file rotator: %v", err)
	}

	pr, pw := io.Pipe()
	go func() {
		err := r.Run(pr)
		fmt.Println("unable to set up logs: ", err)
	}()

	logWriter.RotatorPipe = pw

	return nil
}
