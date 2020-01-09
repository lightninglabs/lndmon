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

	// graphLogger for lndmon's graph collector.
	graphLogger = build.NewSubLogger("GRPH", backendLog.Logger)

	// peerLogger for lndmon's peer collector.
	peerLogger = build.NewSubLogger("PEER", backendLog.Logger)

	// channelLogger for lndmon's channel collector.
	channelLogger = build.NewSubLogger("CHAN", backendLog.Logger)

	// chainLogger for lndmon's chain collector.
	chainLogger = build.NewSubLogger("BLCN", backendLog.Logger)

	// walletLogger for lndmon's wallet collector.
	walletLogger = build.NewSubLogger("WALT", backendLog.Logger)
)

// initLogRotator initializes the logging rotator to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotator variables are used.
func initLogRotator(logFile string, MaxLogFileSize int, MaxLogFiles int) error {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	r, err := rotator.New(
		logFile, int64(MaxLogFileSize*1024), false, MaxLogFiles,
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
