// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// getTxStatus exposes the deprecated [service.GetAtomicTxStatus] endpoint.
func (c *Client) getTxStatus(ctx context.Context, txID ids.ID) (TxStatus, error) {
	var resp TxStatus
	err := c.r.SendRequest(
		ctx,
		"avax.getAtomicTxStatus",
		&api.JSONTxID{
			TxID: txID,
		},
		&resp,
	)
	return resp, err
}

// getAllUTXOs drains [Client.GetUTXOs] for addrs by walking pages of size limit
// until a short page signals the end of the result set.
func (c *Client) getAllUTXOs(
	ctx context.Context,
	tb testing.TB,
	sourceChain ids.ID,
	limit uint32,
	addrs ...ids.ShortID,
) []*avax.UTXO {
	tb.Helper()

	var (
		startAddr   ids.ShortID
		startUTXOID ids.ID
		utxos       []*avax.UTXO
	)
	for {
		page, endAddr, endUTXOID, err := c.GetUTXOs(
			ctx,
			addrs,
			sourceChain,
			limit,
			startAddr,
			startUTXOID,
		)
		require.NoErrorf(tb, err, "%T.GetUTXOs()", c)
		utxos = append(utxos, page...)
		// This termination condition matches the initial API behavior from
		// coreth. Changing the expected termination condition could
		// accidentally break legacy users.
		if uint64(len(page)) < uint64(limit) {
			return utxos
		}
		startAddr, startUTXOID = endAddr, endUTXOID
	}
}

// TestIssueTxRejectsInvalidTransaction asserts that [Client.IssueTx] surfaces
// an error from the transaction pool's verification pipeline.
func TestIssueTxRejectsInvalidTransaction(t *testing.T) {
	ctx, sut := newSUT(t)

	sk := txtest.NewKey(t) // sk is NOT funded.
	w := newWallet(sk, sut.ctx, sut.Client)
	stx := w.newMinimalTx(t)

	err := sut.IssueTx(ctx, stx)
	require.ErrorContainsf(t, err, errIssuingTx.Error(), "%T.IssueTx()", sut.Client)
}

// TestGetTxNotFound asserts that [Client.GetTx] surfaces an error when the
// requested tx has never been accepted.
func TestGetTxNotFound(t *testing.T) {
	ctx, sut := newSUT(t)

	_, _, err := sut.GetTx(ctx, ids.GenerateTestID())
	require.ErrorContainsf(t, err, errFetchingTx.Error(), "%T.GetTx()", sut.Client)
}

// TestGetAtomicTxStatus exercises the deprecated avax.getAtomicTxStatus
// endpoint on both the unknown and accepted branches.
func TestGetAtomicTxStatus(t *testing.T) {
	sk := txtest.NewKey(t)
	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sk.EthAddress())
	}))

	stx := newWallet(sk, sut.ctx, sut.Client).newMinimalTx(t)
	t.Run("before_execution", func(t *testing.T) {
		got, err := sut.getTxStatus(ctx, stx.ID())
		require.NoErrorf(t, err, "%T.getTxStatus()", sut.Client)
		want := TxStatus{
			Status: choices.Unknown,
		}
		require.Equalf(t, want, got, "%T.getTxStatus()", sut.Client)
	})

	blk := sut.issueAndExecute(ctx, t, stx)
	t.Run("after_execution", func(t *testing.T) {
		got, err := sut.getTxStatus(ctx, stx.ID())
		require.NoErrorf(t, err, "%T.getTxStatus()", sut.Client)
		want := TxStatus{
			Status: choices.Accepted,
			Height: utils.PointerTo(json.Uint64(blk.NumberU64())),
		}
		require.Equalf(t, want, got, "%T.getTxStatus()", sut.Client)
	})
}

// TestBlockAndHeaderRPCExposeLibEVMExtras asserts that the block- and
// header-accessible endpoints surface the coreth-specific header and block
// fields injected through libevm's marshalling hooks. The
// eth_get{Header,Block}By{Number,Hash} endpoints route through libevm's
// PostRPCMarshal hook, adding extDataHash, extDataGasUsed, blockGasCost,
// timestampMilliseconds, minDelayExcess (and, for blocks, blockExtraData) — none
// of which exist in vanilla geth's JSON output. The newHeads subscription takes a
// different path, serializing the raw header via [customtypes.HeaderExtra]'s
// JSON/RLP hooks, and is covered here too.
func TestBlockAndHeaderRPCExposeLibEVMExtras(t *testing.T) {
	key := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))
	w := newWallet(key, sut.ctx, sut.Client)

	// A cross-chain export gives the built block non-empty extData, so extDataHash
	// and blockExtraData carry meaningful (non-default) values rather than the
	// empty-block defaults.
	blk := sut.issueAndExecute(ctx, t, w.newMinimalTx(t))

	hdr := blk.Header()
	extra := customtypes.GetHeaderExtra(hdr)
	require.NotNil(t, extra.ExtDataGasUsed, "header ExtDataGasUsed")
	require.NotNil(t, extra.BlockGasCost, "header BlockGasCost")
	require.NotNil(t, extra.TimeMilliseconds, "header TimeMilliseconds")
	require.NotNil(t, extra.MinDelayExcess, "header MinDelayExcess")

	// The keys and encodings here mirror coreth's PostRPCMarshal hooks exactly.
	wantHeaderExtras := map[string]any{
		"extDataHash":           extra.ExtDataHash.Hex(),
		"extDataGasUsed":        (*hexutil.Big)(extra.ExtDataGasUsed).String(),
		"blockGasCost":          (*hexutil.Big)(extra.BlockGasCost).String(),
		"timestampMilliseconds": hexutil.Uint64(*extra.TimeMilliseconds).String(),
		"minDelayExcess":        hexutil.Uint64(*extra.MinDelayExcess).String(),
	}
	wantBlockExtraData := hexutil.Bytes(customtypes.BlockExtData(blk.EthBlock())).String()

	rpcClient := sut.ethclient.Client()
	var (
		blockNumber = hexutil.EncodeUint64(blk.NumberU64())
		blockHash   = blk.EthBlock().Hash()
	)

	// call invokes method with args and returns the decoded JSON response.
	call := func(t *testing.T, method string, args ...any) map[string]any {
		t.Helper()
		var got map[string]any
		err := rpcClient.CallContext(ctx, &got, method, args...)
		require.NoErrorf(t, err, "%s", method)
		return got
	}

	// assertHeaderExtras asserts that resp carries each PostRPCMarshal header field.
	assertHeaderExtras := func(t *testing.T, resp map[string]any) {
		t.Helper()
		for k, want := range wantHeaderExtras {
			require.Equalf(t, want, resp[k], "field %q", k)
		}
	}

	// Header endpoints expose only the HeaderExtra hook fields, addressable by
	// both number and hash.
	headerMethods := map[string][]any{
		"eth_getHeaderByNumber": {blockNumber},
		"eth_getHeaderByHash":   {blockHash},
	}
	for method, args := range headerMethods {
		t.Run(method, func(t *testing.T) {
			assertHeaderExtras(t, call(t, method, args...))
		})
	}

	// Block endpoints embed the header fields and additionally expose
	// blockExtraData via the block hook, addressable by both number and hash.
	blockMethods := map[string][]any{
		"eth_getBlockByNumber": {blockNumber, true},
		"eth_getBlockByHash":   {blockHash, true},
	}
	for method, args := range blockMethods {
		t.Run(method, func(t *testing.T) {
			resp := call(t, method, args...)
			assertHeaderExtras(t, resp)
			require.Equal(t, wantBlockExtraData, resp["blockExtraData"], "field blockExtraData")
		})
	}

	// The newHeads subscription delivers the raw header, marshalled via
	// [customtypes.HeaderExtra]'s JSON hook rather than PostRPCMarshal.
	t.Run("eth_subscribe(newHeads)", func(t *testing.T) {
		heads := make(chan *types.Header, 1)
		sub, err := sut.ethclient.SubscribeNewHead(ctx, heads)
		require.NoError(t, err, "SubscribeNewHead")
		defer sub.Unsubscribe()

		// [filters.FilterAPI.NewHeads] installs the underlying chain-head
		// subscription inside a goroutine spawned *after* the RPC call returns,
		// so a head produced immediately after SubscribeNewHead returns can race
		// that installation and be dropped. Keep producing blocks until the
		// subscription delivers one; the installation completes well within the
		// first iteration, so any later block is guaranteed to be observed.
		var got *types.Header
		for attempts := 0; got == nil && attempts < 5; attempts++ {
			sut.issueAndExecute(ctx, t, w.newMinimalTx(t))
			select {
			case got = <-heads:
			case err := <-sub.Err():
				require.FailNowf(t, "subscription error", "%v", err)
			case <-time.After(2 * time.Second):
				// Lost the installation race; the next block will be observed.
			}
		}
		require.NotNil(t, got, "no new head delivered over subscription")

		// Cross-check the delivered header's extras against the already-verified
		// by-hash endpoint for the same block: identical re-encoded values prove
		// the subscription's JSON hook carries the extras faithfully.
		canonical := call(t, "eth_getHeaderByHash", got.Hash())
		gotExtra := customtypes.GetHeaderExtra(got)
		require.NotNil(t, gotExtra.ExtDataGasUsed, "extDataGasUsed")
		require.NotNil(t, gotExtra.BlockGasCost, "blockGasCost")
		require.NotNil(t, gotExtra.TimeMilliseconds, "timestampMilliseconds")
		require.NotNil(t, gotExtra.MinDelayExcess, "minDelayExcess")
		require.Equal(t, canonical["extDataHash"], gotExtra.ExtDataHash.Hex(), "extDataHash")
		require.Equal(t, canonical["extDataGasUsed"], (*hexutil.Big)(gotExtra.ExtDataGasUsed).String(), "extDataGasUsed")
		require.Equal(t, canonical["blockGasCost"], (*hexutil.Big)(gotExtra.BlockGasCost).String(), "blockGasCost")
		require.Equal(t, canonical["timestampMilliseconds"], hexutil.Uint64(*gotExtra.TimeMilliseconds).String(), "timestampMilliseconds")
		require.Equal(t, canonical["minDelayExcess"], hexutil.Uint64(*gotExtra.MinDelayExcess).String(), "minDelayExcess")
	})
}

// TestGetUTXOsPagination asserts that walking [Client.GetUTXOs] yields each
// seeded UTXO exactly once.
func TestGetUTXOsPagination(t *testing.T) {
	ctx, sut := newSUT(t)

	sourceChain := sut.ctx.XChainID
	const numUTXOs uint64 = 5
	want := make([]*avax.UTXO, numUTXOs)
	addr := txtest.NewKey(t).Address()
	for i := range numUTXOs {
		want[i] = txtest.NewUTXO(i+1, sut.ctx.AVAXAssetID, addr)
	}
	sut.addUTXOs(t, sut.ctx.ChainID, sourceChain, want...)

	// pageSize=1 stresses the boundary behavior so any off-by-one in the cursor
	// logic will surface here.
	const pageSize = 1
	got := sut.Client.getAllUTXOs(ctx, t, sourceChain, pageSize, addr)
	if diff := cmp.Diff(want, got, txtest.UTXOCmpOpt()); diff != "" {
		t.Errorf("paginated UTXOs (-want +got):\n%s", diff)
	}
}
