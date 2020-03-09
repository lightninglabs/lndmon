package lndmon

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	flags "github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndmon/collectors"
	"github.com/lightninglabs/loop/lndclient"
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

	// Initialize our lnd client.
	lnd, err := lndclient.NewBasicClient(
		cfg.Lnd.Host, cfg.Lnd.TLSPath, cfg.Lnd.MacaroonDir,
		cfg.Lnd.Network, lndclient.MacFilename("readonly.macaroon"),
	)
	if err != nil {
		return err
	}

	// Start our Prometheus exporter. This exporter spawns a goroutine
	// that pulls metrics from our lnd client on a set interval.
	exporter := collectors.NewPrometheusExporter(cfg.Prometheus, lnd)

	listenErr := make(chan error, 1)
	if err := exporter.Start(listenErr); err != nil {
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

	select {
	case <-done:
	case err := <-listenErr:
		if err != nil {
			return fmt.Errorf("received error from Prometheus exporter: %w", err)
		}
	}

	fmt.Println("Exiting lndmon.")

	return nil
}
