module github.com/lightninglabs/lndmon

require (
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/grpc-ecosystem/grpc-gateway v1.9.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lightninglabs/loop v0.1.1-alpha.0.20190522001358-86264db1a33f
	github.com/lightningnetwork/lnd v0.6.1-beta.0.20190605130338-880279b266e9
	github.com/prometheus/client_golang v0.9.2
	google.golang.org/grpc v1.20.1 // indirect
)

replace github.com/lightninglabs/loop => github.com/valentinewallace/loop v0.1.1-alpha.0.20190606225725-8311d70cbf8b
