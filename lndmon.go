package lndmon

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	flags "github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/lndmon/collectors"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"github.com/lightningnetwork/lnd/routing/route"
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

	// Initialize our lnd client, requiring at least lnd v0.11.
	lnd, err := lndclient.NewLndServices(
		&lndclient.LndServicesConfig{
			LndAddress:         cfg.Lnd.Host,
			Network:            lndclient.Network(cfg.Lnd.Network),
			CustomMacaroonPath: "/root/.lnd/readonly.macaroon",
			TLSPath:            cfg.Lnd.TLSPath,
			CheckVersion: &verrpc.Version{
				AppMajor: 0,
				AppMinor: 11,
				AppPatch: 0,
			},
		},
	)
	if err != nil {
		return err
	}
	defer lnd.Close()

	monitoringCfg := collectors.MonitoringConfig{}
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

	// Wait for a signal to exit.
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		fmt.Printf("Received quit signal: %v\n", sig)
		done <- true
	}()

	<-done
	fmt.Println("Exiting lndmon.")

	return nil
}
