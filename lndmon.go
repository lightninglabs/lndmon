package lndmon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	flags "github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/lndmon/collectors"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/signal"
)

// Main is the true entrypoint to lndmon.
func Main() {
	// TODO: Prevent from running twice.
	err := start()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func start() error {
	if _, err := flags.Parse(&cfg); err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			return nil
		}
		return err
	}

	quit := make(chan struct{})
	interceptor, err := signal.Intercept()
	if err != nil {
		return fmt.Errorf("could not intercept signal: %v", err)
	}

	programStartTime := time.Now()

	// Initialize our lnd client, requiring at least lnd v0.11.
	lnd, err := lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress: cfg.Lnd.Host,
			Network:    lndclient.Network(cfg.Lnd.Network),
			CustomMacaroonPath: filepath.Join(
				cfg.Lnd.MacaroonDir, cfg.Lnd.MacaroonName,
			),
			RPCTimeout: cfg.Lnd.RPCTimeout,
			TLSPath:    cfg.Lnd.TLSPath,
			CheckVersion: &verrpc.Version{
				AppMajor: 0,
				AppMinor: 13,
			},
			BlockUntilUnlocked: true,
		},
	)
	if err != nil {
		return err
	}
	defer lnd.Close()

	monitoringCfg := collectors.MonitoringConfig{
		DisableGraph:    cfg.DisableGraph,
		DisableHtlc:     cfg.DisableHtlc,
		DisablePayments: cfg.DisablePayments,
	}
	if cfg.PrimaryNode != "" {
		primaryNode, err := route.NewVertexFromStr(cfg.PrimaryNode)
		if err != nil {
			return err
		}
		monitoringCfg.PrimaryNode = &primaryNode
	}
	monitoringCfg.ProgramStartTime = programStartTime

	// Start our Prometheus exporter. This exporter spawns a goroutine
	// that pulls metrics from our lnd client on a set interval.
	exporter := collectors.NewPrometheusExporter(
		cfg.Prometheus, &lnd.LndServices, &monitoringCfg, quit,
	)
	if err := exporter.Start(); err != nil {
		return err
	}

	// Wait to get the signal to shutdown, or for an error to occur with
	// our metric export.
	var stopErr error
	select {
	case <-interceptor.ShutdownChannel():
		close(quit)
		fmt.Println("Exiting lndmon.")

	case stopErr = <-exporter.Errors():
		fmt.Printf("Lndmon exiting with error: %v\n", stopErr)
	}

	// Before we exit, stop our prometheus exporter, then return the error
	// we originally exited for (if any).
	exporter.Stop()

	return stopErr
}
