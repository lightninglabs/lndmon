package config

import (
	"path/filepath"

	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/lndmon/health"
)

var (
	// log configuration defaults.
	defaultLogFilename = "lndmon.log"
	defaultLndmonDir   = btcutil.AppDataDir("lndmon", false)
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

// PrometheusConfig is the set of configuration data that specifies the
// listening address of the Prometheus server and configuration for lndmon logs.
type PrometheusConfig struct {
	// ListenAddr is the listening address that we should use to allow the
	// main Prometheus server to scrape our metrics.
	ListenAddr string `long:"listenaddr" description:"the interface we should listen on for prometheus"`

	// LogDir is the directory to log lndmon output.
	LogDir string `long:"logdir" description:"Directory to log output"`

	// MaxLogFiles is the maximum number of log files to keep (0 for no
	// rotation).
	MaxLogFiles int `long:"maxlogfiles" description:"Maximum log files to keep (0 for no rotation)"`

	// MaxLogFileSize is the maximum log file size in MB.
	MaxLogFileSize int `long:"maxlogfilesize" description:"Maximum log file size in MB"`
}

type ChannelConfig struct {
	// Minimum channel size to export metrics for
	MinChanSize int64 `long:"minsize" description:"Minimum channel size to export metrics"`
}

type Config struct {
	// Prometheus specifies the listening address of the Prometheus server.
	Prometheus *PrometheusConfig `group:"prometheus" namespace:"prometheus"`

	// Lnd refers to the user's lnd configuration properties that we need to
	// connect to it.
	Lnd *lndConfig `group:"lnd" namespace:"lnd"`

	// Channel specifices filters on what channel metrics to collect.
	Channels *ChannelConfig `group:"channels" namespace:"channels"`
}

var DefaultConfig = Config{
	Prometheus: &PrometheusConfig{
		ListenAddr:     "localhost:9092",
		LogDir:         filepath.Join(defaultLndmonDir, "logs"),
		MaxLogFiles:    3,
		MaxLogFileSize: 10,
	},
	Lnd: &lndConfig{
		Host:    "localhost:10009",
		Network: "mainnet",
	},
	Channels: &ChannelConfig{
		MinChanSize: 0,
	},
	Health: health.DefaultConfig(),
}
