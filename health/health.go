package health

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/lightningnetwork/lnd/lnrpc"
)

type HealthChecker struct {
	cfg *HealthConfig

	lnd lnrpc.LightningClient
}

type HealthConfig struct {
	Active     bool   `long:"active" description:"If the health check should be active or not"`
	Path       string `long:"path" description:"path to listen for healtcheck requests"`
	ListenAddr string `long:"port" description:"the interface we should listen on for health checks"`
}

func DefaultConfig() *HealthConfig {
	return &HealthConfig{
		Active:     false,
		Path:       "/health",
		ListenAddr: "localhost:8081",
	}
}

func NewHealthChecker(cfg *HealthConfig, lnd lnrpc.LightningClient) *HealthChecker {
	return &HealthChecker{
		cfg: cfg,
		lnd: lnd,
	}
}

func (hc *HealthChecker) Start() error {
	// We launch a HTTP server that can serve health check requests
	go func() {
		http.Handle(hc.cfg.Path, hc)
		http.ListenAndServe(hc.cfg.ListenAddr, nil)
	}()

	return nil
}

func (hc *HealthChecker) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Check our lnd node is available and working
	_, err := hc.lnd.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		http.Error(w, err.Error(), 500)
	}
}
