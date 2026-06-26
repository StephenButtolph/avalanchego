// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transition

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
)

func TestTransition(t *testing.T) {
	evm.RegisterAllLibEVMExtras()
	ginkgo.RunSpecs(t, "transition e2e test suites")
}

var (
	avalancheGoExecPath   string
	activationDelay       time.Duration
	collectorVars         *flags.CollectorVars
	checkMetricsCollected bool
	checkLogsCollected    bool
)

func init() {
	flag.StringVar(
		&avalancheGoExecPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.DurationVar(
		&activationDelay,
		"latest-activation-delay",
		e2e.DefaultTransitionActivationDelay,
		"the interval after network configuration at which the latest upgrade should activate. Must comfortably exceed network bootstrap time.",
	)
	collectorVars = flags.NewCollectorFlagVars()
	e2e.SetCheckCollectionFlags(
		&checkMetricsCollected,
		&checkLogsCollected,
	)
}

var _ = ginkgo.Describe("[Latest Upgrade]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("tests the activation of the latest upgrade", func() {
		// The latest upgrade is activated while the network is running, with all
		// prior forks active from genesis.
		const targetFork = upgradetest.Latest

		network := tmpnet.NewDefaultNetwork("avalanchego-transition")
		network.DefaultRuntimeConfig = tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				AvalancheGoPath: avalancheGoExecPath,
			},
		}

		tc.By(fmt.Sprintf("configuring the network to activate the latest upgrade (%s) while running", targetFork))
		upgradeFlags, latestUpgradeTime := e2e.NewTransitionUpgradeFlags(tc, targetFork, activationDelay)
		network.DefaultFlags = tmpnet.FlagsMap{}
		network.DefaultFlags.SetDefaults(upgradeFlags)
		network.DefaultFlags.SetDefaults(tmpnet.DefaultE2EFlags())

		shutdownDelay := 0 * time.Second
		if collectorVars.StartMetricsCollector {
			require.NoError(tmpnet.StartPrometheus(tc.DefaultContext(), tc.Log()))
			shutdownDelay = tmpnet.NetworkShutdownDelay // Ensure a final metrics scrape
		}
		if collectorVars.StartLogsCollector {
			require.NoError(tmpnet.StartPromtail(tc.DefaultContext(), tc.Log()))
		}

		// Since cleanups are run in LIFO order, adding these cleanups before
		// StartNetwork is called ensures network shutdown will be called first.
		if checkMetricsCollected {
			tc.DeferCleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckMetricsExist(ctx, tc.Log(), network.UUID))
			})
		}
		if checkLogsCollected {
			tc.DeferCleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckLogsExist(ctx, tc.Log(), network.UUID))
			})
		}

		tc.By("starting the network")
		e2e.StartNetwork(
			tc,
			network,
			"", /* rootNetworkDir */
			shutdownDelay,
			e2e.EmptyNetworkCmd,
		)

		var (
			node      = network.Nodes[0]
			nodeURI   = tmpnet.NodeURI{NodeID: node.NodeID, URI: node.GetAccessibleURI()}
			senderKey = network.PreFundedKeys[0]
			ethClient = e2e.NewEthClient(tc, nodeURI)
		)

		// Bootstrap must complete with time to spare before the upgrade so that the
		// pre-upgrade transactions are issued before it activates.
		require.True(
			time.Now().Before(latestUpgradeTime),
			"network bootstrap consumed the pre-upgrade window; increase --latest-activation-delay",
		)

		tc.By("issuing C-Chain transactions before the latest upgrade")
		issueEthTransfer(tc, ethClient, senderKey)
		issueEthTransfer(tc, ethClient, senderKey)
		preUpgradeBlockNumber, err := ethClient.BlockNumber(tc.DefaultContext())
		require.NoError(err)
		tc.Log().Info("issued transactions before the latest upgrade",
			zap.Uint64("blockNumber", preUpgradeBlockNumber),
		)

		// Keep the chain under active load as it swaps VMs by continuously issuing
		// transactions until a block is produced at or after the upgrade time,
		// rather than letting the network transition while idle. issueEthTransfer
		// blocks until each transaction is accepted, so the loop is self-paced.
		tc.By("issuing C-Chain transactions until the chain transitions to the latest upgrade")
		for {
			receipt := issueEthTransfer(tc, ethClient, senderKey)

			header, err := ethClient.HeaderByNumber(tc.DefaultContext(), receipt.BlockNumber)
			require.NoError(err)
			if header.Time >= uint64(latestUpgradeTime.Unix()) {
				break
			}
		}

		tc.By("issuing C-Chain transactions after the latest upgrade")
		issueEthTransfer(tc, ethClient, senderKey)
		issueEthTransfer(tc, ethClient, senderKey)

		tc.By("confirming the C-Chain continued to produce blocks across the upgrade")
		postUpgradeBlockNumber, err := ethClient.BlockNumber(tc.DefaultContext())
		require.NoError(err)
		require.Greater(
			postUpgradeBlockNumber,
			preUpgradeBlockNumber,
			"expected the C-Chain to keep producing blocks after the upgrade",
		)
		tc.Log().Info("issued transactions after the latest upgrade",
			zap.Uint64("blockNumber", postUpgradeBlockNumber),
		)

		tc.By("confirming a new node can bootstrap the chain across the upgrade")
		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})

// issueEthTransfer issues a self-transfer eth transaction on the C-Chain, waits
// for it to be accepted, and returns its receipt.
func issueEthTransfer(
	tc tests.TestContext,
	ethClient *ethclient.Client,
	senderKey *secp256k1.PrivateKey,
) *types.Receipt {
	ctx := tc.DefaultContext()
	addr := senderKey.EthAddress()
	nonce, err := ethClient.AcceptedNonceAt(ctx, addr)
	require.NoError(tc, err)

	gasPrice := e2e.SuggestGasPrice(tc, ethClient)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &addr, // self-transfer to avoid depending on a separate recipient
		Value:    new(big.Int).SetUint64(units.Avax),
		Gas:      e2e.DefaultGasLimit,
		GasPrice: gasPrice,
	})

	cChainID, err := ethClient.ChainID(ctx)
	require.NoError(tc, err)
	signer := types.LatestSignerForChainID(cChainID)
	signedTx, err := types.SignTx(tx, signer, senderKey.ToECDSA())
	require.NoError(tc, err)

	receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
	require.Equal(tc, types.ReceiptStatusSuccessful, receipt.Status)
	return receipt
}
