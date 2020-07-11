package lndmon

import (
	"github.com/lightninglabs/lndmon/collectors"
)

type lndConfig struct {
	// Host is the RPC address of the lnd instance that lndmon is connecting
	// to.
	Host string `long:"host" description:"lnd instance rpc address"`

	// Network is the network that lnd is running on, i.e. mainnet.
	Network string `long:"network" description:"network to run on" choice:"regtest" choice:"testnet" choice:"mainnet" choice:"simnet"`

	// MacaroonDir is the path to lnd macaroons.
	MacaroonDir string `long:"macaroondir" description:"Path to lnd macaroons"`

	// TLSPath is the path to the lnd TLS certificate.
	TLSPath string `long:"tlspath" description:"Path to lnd tls certificate"`
}

type config struct {
	// Prometheus specifies the listening address of the Prometheus server.
	Prometheus *collectors.PrometheusConfig `group:"prometheus" namespace:"prometheus"`

	// Lnd refers to the user's lnd configuration properties that we need to
	// connect to it.
	Lnd *lndConfig `group:"lnd" namespace:"lnd"`

	// PrimaryNode is the pubkey of the primary node in primary-gateway setups.
	PrimaryNode string `long:"primarynode" description:"Public key of the primary node in a primary-gateway setup"`
}

var defaultConfig = config{
	Prometheus: collectors.DefaultConfig(),
	Lnd: &lndConfig{
		Host:    "localhost:10009",
		Network: "mainnet",
	},
}

var (
	cfg = defaultConfig
)
