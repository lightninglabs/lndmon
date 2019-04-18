package lndclient

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	macaroon "gopkg.in/macaroon.v2"
)

const (
	defaultRPCPort = "10009"
	macPath        = "/admin.macaroon"
	tlsPath        = "/tls.cert"
)

var (
	rpcTimeout = 30 * time.Second
)

type LndClient struct {
	ctx    context.Context
	client lnrpc.LightningClient
}

func New() (*LndClient, error) {
	address, err := lncfg.ParseAddressString(
		os.Getenv("HOST_ADDR"), defaultRPCPort, net.ResolveTCPAddr,
	)
	conn, err := getClientConn(address.String(), macPath, tlsPath)
	if err != nil {
		return nil, err
	}

	return &LndClient{
		ctx:    context.Background(),
		client: lnrpc.NewLightningClient(conn),
	}, nil
}

func (c *LndClient) GetInfo() (*lnrpc.GetInfoResponse, error) {
	rpcCtx, cancel := context.WithTimeout(c.ctx, rpcTimeout)
	defer cancel()

	resp, err := c.client.GetInfo(rpcCtx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *LndClient) ChannelBalance() (*lnrpc.ChannelBalanceResponse, error) {
	rpcCtx, cancel := context.WithTimeout(c.ctx, rpcTimeout)
	defer cancel()

	resp, err := c.client.ChannelBalance(rpcCtx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *LndClient) ListChannels() (*lnrpc.ListChannelsResponse, error) {
	rpcCtx, cancel := context.WithTimeout(c.ctx, rpcTimeout)
	defer cancel()

	resp, err := c.client.ListChannels(rpcCtx, &lnrpc.ListChannelsRequest{})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *LndClient) FeeReport() (*lnrpc.FeeReportResponse, error) {
	rpcCtx, cancel := context.WithTimeout(c.ctx, rpcTimeout)
	defer cancel()

	resp, err := c.client.FeeReport(rpcCtx, &lnrpc.FeeReportRequest{})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func getClientConn(address string, macPath, tlsPath string) (*grpc.ClientConn, error) {
	rpcPort := strings.Split(address, ":")[1]
	creds, err := credentials.NewClientTLSFromFile(tlsPath, "")
	if err != nil {
		return nil, err
	}

	// Create a dial options array.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	// Load the specified macaroon file.
	macBytes, err := ioutil.ReadFile(macPath)
	if err == nil {
		// Only if file is found
		mac := &macaroon.Macaroon{}
		if err = mac.UnmarshalBinary(macBytes); err != nil {
			return nil, fmt.Errorf("unable to decode macaroon: %v",
				err)
		}

		// Now we append the macaroon credentials to the dial options.
		cred := macaroons.NewMacaroonCredential(mac)
		opts = append(opts, grpc.WithPerRPCCredentials(cred))
	}

	// We need to use a custom dialer so we can also connect to unix sockets
	// and not just TCP addresses.
	opts = append(
		opts, grpc.WithDialer(
			lncfg.ClientAddressDialer(rpcPort),
		),
	)
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server: %v", err)
	}

	return conn, nil
}
