package collectors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type invoicePageClient struct {
	lnrpc.LightningClient

	responses []*lnrpc.ListInvoiceResponse
	requests  []*lnrpc.ListInvoiceRequest
	failAt    int
	deadlines []bool
}

func (c *invoicePageClient) ListInvoices(ctx context.Context,
	request *lnrpc.ListInvoiceRequest,
	_ ...grpc.CallOption) (*lnrpc.ListInvoiceResponse, error) {

	c.requests = append(c.requests, request)
	_, deadline := ctx.Deadline()
	c.deadlines = append(c.deadlines, deadline)
	if c.failAt > 0 && len(c.requests) == c.failAt {
		return nil, errors.New("invoice RPC unavailable")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestInvoiceSnapshotUsesBoundedInventory(t *testing.T) {
	client := &invoicePageClient{
		responses: []*lnrpc.ListInvoiceResponse{
			{Invoices: []*lnrpc.Invoice{{AddIndex: 10}}},
			{
				LastIndexOffset: 3,
				Invoices: []*lnrpc.Invoice{
					{AddIndex: 1, State: lnrpc.Invoice_OPEN, ValueMsat: 1500},
					{AddIndex: 3, State: lnrpc.Invoice_ACCEPTED, ValueMsat: 2500},
				},
			},
			{
				LastIndexOffset: 11,
				Invoices: []*lnrpc.Invoice{
					{AddIndex: 6, State: lnrpc.Invoice_CANCELED, ValueMsat: 3500},
					{
						AddIndex: 10, State: lnrpc.Invoice_SETTLED,
						IsAmp: true, AmtPaidMsat: 11250,
						AmpInvoiceState: map[string]*lnrpc.AMPInvoiceState{
							"first":  {State: lnrpc.InvoiceHTLCState_SETTLED, AmtPaidMsat: 5000},
							"second": {State: lnrpc.InvoiceHTLCState_SETTLED, AmtPaidMsat: 6250},
						},
					},
					{AddIndex: 11, State: lnrpc.Invoice_OPEN, ValueMsat: 9000},
				},
			},
		},
	}
	monitor := &invoicesMonitor{client: client, rpcTimeout: time.Second}

	snapshot, err := monitor.readSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, [invoiceStateCount]uint64{1, 1, 1, 1, 0}, snapshot.counts)
	require.Equal(t, [invoiceStateCount]float64{1.5, 11.25, 3.5, 2.5, 0}, snapshot.sats)
	require.Len(t, client.requests, 3)
	require.True(t, client.requests[0].Reversed)
	require.EqualValues(t, 1, client.requests[0].NumMaxInvoices)
	require.EqualValues(t, 0, client.requests[1].IndexOffset)
	require.EqualValues(t, 3, client.requests[2].IndexOffset)
	require.Equal(t, []bool{true, true, true}, client.deadlines)
}

func TestInvoiceSnapshotRejectsMalformedPagination(t *testing.T) {
	cases := []struct {
		name string
		page *lnrpc.ListInvoiceResponse
	}{
		{
			name: "duplicate invoice",
			page: &lnrpc.ListInvoiceResponse{
				LastIndexOffset: 1,
				Invoices:        []*lnrpc.Invoice{{AddIndex: 1}, {AddIndex: 1}},
			},
		},
		{
			name: "missing cursor",
			page: &lnrpc.ListInvoiceResponse{
				Invoices: []*lnrpc.Invoice{{AddIndex: 1}},
			},
		},
		{
			name: "cursor skips unseen invoices",
			page: &lnrpc.ListInvoiceResponse{
				LastIndexOffset: 10,
				Invoices:        []*lnrpc.Invoice{{AddIndex: 1}},
			},
		},
		{
			name: "nil invoice",
			page: &lnrpc.ListInvoiceResponse{
				LastIndexOffset: 1,
				Invoices:        []*lnrpc.Invoice{nil},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var snapshot invoiceSnapshot
			require.Error(t, snapshot.addPage(testCase.page, 0, 10))
		})
	}
}

func TestInvoiceSnapshotFailureDoesNotPublishPartialData(t *testing.T) {
	client := &invoicePageClient{
		failAt: 3,
		responses: []*lnrpc.ListInvoiceResponse{
			{Invoices: []*lnrpc.Invoice{{AddIndex: 10}}},
			{
				LastIndexOffset: 1,
				Invoices:        []*lnrpc.Invoice{{AddIndex: 1}},
			},
		},
	}
	monitor := &invoicesMonitor{client: client, rpcTimeout: time.Second}
	previous := invoiceSnapshot{counts: [invoiceStateCount]uint64{3}}
	monitor.publish(previous)

	partial, err := monitor.readSnapshot(context.Background())
	require.Error(t, err)
	require.Equal(t, invoiceSnapshot{}, partial)
	current, err := monitor.currentSnapshot()
	require.NoError(t, err)
	require.Equal(t, previous, current)

	monitor.invalidate(errors.New("refresh failed"))
	_, err = monitor.currentSnapshot()
	require.Error(t, err)
}

func TestInvoiceSnapshotEmptyInventory(t *testing.T) {
	client := &invoicePageClient{
		responses: []*lnrpc.ListInvoiceResponse{{}},
	}
	monitor := &invoicesMonitor{client: client, rpcTimeout: time.Second}

	snapshot, err := monitor.readSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, invoiceSnapshot{}, snapshot)
	require.Len(t, client.requests, 1)
}
