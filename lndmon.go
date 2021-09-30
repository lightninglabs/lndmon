package lndmon

import (
	"fmt"
	"os"
	"path/filepath"

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

	interceptor, err := signal.Intercept()
	if err != nil {
		return fmt.Errorf("could not intercept signal: %v", err)
	}

	// Initialize our lnd client, requiring at least lnd v0.11.
	lnd, err := lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress: cfg.Lnd.Host,
			Network:    lndclient.Network(cfg.Lnd.Network),
			CustomMacaroonPath: filepath.Join(
				cfg.Lnd.MacaroonDir, cfg.Lnd.MacaroonName,
			),
			TLSPath: cfg.Lnd.TLSPath,
			CheckVersion: &verrpc.Version{
				AppMajor: 0,
				AppMinor: 13,
			},
		},
	)
	if err != nil {
		return err
	}
	defer lnd.Close()

	monitoringCfg := collectors.MonitoringConfig{
		DisableGraph: cfg.DisableGraph,
	}
	if cfg.PrimaryNode != "" {
		primaryNode, err := route.NewVertexFromStr(cfg.PrimaryNode)
		if err != nil {
			return err
		}
		monitoringCfg.PrimaryNode = &primaryNode
	}

	// Start our Prometheus exporter. This exporter spawns a goroutine
	// that pulls metrics from our lnd client on a set interval.
	exporter := collectors.NewPrometheusExporter(
		cfg.Prometheus, &lnd.LndServices, &monitoringCfg,
	)
	if err := exporter.Start(); err != nil {
		return err
	}

	// Wait to get the signal to shutdown, or for an error to occur with
	// our metric export.
	var stopErr error
	select {
	case <-interceptor.ShutdownChannel():
		fmt.Println("Exiting lndmon.")

	case stopErr = <-exporter.Errors():
		fmt.Printf("Lndmon exiting with error: %v\n", stopErr)
	}

	// Before we exit, stop our prometheus exporter, then return the error
	// we originally exited for (if any).
	exporter.Stop()

	return stopErr
}
