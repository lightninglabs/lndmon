package collectors

import (
	"testing"

	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/stretchr/testify/require"
)

var (
	remotePolicies = map[uint64]*lndclient.RoutingPolicy{
		1: {
			FeeBaseMsat:      20000,
			FeeRateMilliMsat: 10000,
		},
		2: {
			FeeBaseMsat:      250000,
			FeeRateMilliMsat: 6000,
		},
	}

	remoteBalances = map[uint64]btcutil.Amount{
		1: 10000,
		2: 10000,
	}
)

// TestGetInboundFee tests the specific-fee based inbound fee calculation.
func TestGetInboundFee(t *testing.T) {
	testCases := []struct {
		name              string
		amt               btcutil.Amount
		expectedFee       btcutil.Amount
		expectNoLiquidity bool
	}{
		{
			name:        "single channel use all",
			amt:         10000,
			expectedFee: 120,
		},
		{
			name:        "single channel partially used",
			amt:         5000,
			expectedFee: 70,
		},
		{
			name:              "not enough",
			amt:               25000,
			expectNoLiquidity: true,
		},
		{
			name:        "two channels use all",
			amt:         20000,
			expectedFee: 120 + 310,
		},
		{
			name:        "two channels partially used",
			amt:         15000,
			expectedFee: 120 + 280,
		},
	}

	for _, test := range testCases {
		test := test

		t.Run(test.name, func(t *testing.T) {
			testGetInboundFee(
				t, test.amt, test.expectedFee,
				test.expectNoLiquidity,
			)
		})
	}
}

func testGetInboundFee(t *testing.T, amt, expectedFee btcutil.Amount,
	expectNoLiquidity bool) {

	fee := approximateInboundFee(amt, remotePolicies, remoteBalances)

	if expectNoLiquidity {
		require.Nil(t, fee, "expected no liquidity")
		return
	}

	require.NotNil(t, fee, "expected routing to be possible")
	require.Equal(t, expectedFee, *fee)
}
