package lndmon

import (
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndmon/collectors"
)

var (
	// defaultMacaroonDir is the default path that we use to point lndmon
	// to our macaroon. This default works for using lndmon in our docker
	// compose setup.
	defaultMacaroonDir = btcutil.AppDataDir("lnd", false)

	// defaultMacaroon is the default macaroon that we use for lndmon.
	defaultMacaroon = "readonly.macaroon"
)

type lndConfig struct {
	// Host is the RPC address of the lnd instance that lndmon is connecting
	// to.
	Host string `long:"host" description:"lnd instance rpc address"`

	// Network is the network that lnd is running on, i.e. mainnet.
	Network string `long:"network" description:"network to run on" choice:"regtest" choice:"testnet" choice:"testnet4" choice:"mainnet" choice:"simnet" choice:"signet"`

	// MacaroonDir is the path to lnd macaroons.
	MacaroonDir string `long:"macaroondir" description:"Path to lnd macaroons"`

	// MacaroonName is the name of the macaroon in macaroon dir to use.
	MacaroonName string `long:"macaroonname" description:"The name of our macaroon in macaroon dir to use."`

	// RPCTimeout is the timeout for rpc calls to lnd.
	RPCTimeout time.Duration `long:"rpctimeout" description:"The timeout for rpc calls to lnd. Valid time units are {s, m, h}."`

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

	// DisableGraph disables collection of graph metrics.
	DisableGraph bool `long:"disablegraph" description:"Do not collect graph metrics"`

	// DisableHtlc disables the collection of HTLCs metrics.
	DisableHtlc bool `long:"disablehtlc" description:"Do not collect HTLCs metrics"`

	// DisablePayments disables the collection of payments metrics.
	DisablePayments bool `long:"disablepayments" description:"Do not collect payments metrics"`
}

var defaultConfig = config{
	Prometheus: collectors.DefaultConfig(),
	Lnd: &lndConfig{
		Host:         "localhost:10009",
		Network:      "mainnet",
		MacaroonDir:  defaultMacaroonDir,
		MacaroonName: defaultMacaroon,
		RPCTimeout:   30 * time.Second,
	},
}

var (
	cfg = defaultConfig
)
