// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extdata

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	os.Exit(m.Run())
}

// newBlock builds a block at the given height whose header commits extDataHash
// and whose body carries extData, without recomputing the hash, so a test can
// pair any (height, extDataHash, extData) combination.
func newBlock(height uint64, extDataHash common.Hash, extData []byte) *types.Block {
	header := customtypes.WithHeaderExtra(
		&types.Header{Number: new(big.Int).SetUint64(height)},
		&customtypes.HeaderExtra{ExtDataHash: extDataHash},
	)
	block := types.NewBlock(header, nil, nil, nil, trie.NewStackTrie(nil))
	customtypes.SetBlockExtra(block, &customtypes.BlockBodyExtra{ExtData: &extData})
	return block
}

func TestVerifyExtDataHash(t *testing.T) {
	var (
		empty        = common.Hash{}
		extData      = []byte{1, 2, 3}
		extDataHash  = customtypes.CalcExtDataHash(extData)
		emptyExtHash = customtypes.CalcExtDataHash(nil)
	)

	const (
		recordedHeight   = 10
		unrecordedHeight = 20
	)
	// The recorded set pins the extData of recordedHeight; unrecordedHeight is
	// absent from it.
	hashes := map[uint64]common.Hash{recordedHeight: extDataHash}

	tests := []struct {
		name            string
		isApricotPhase1 bool
		block           *types.Block
		hashes          map[uint64]common.Hash
		wantErr         error
	}{
		{
			// ApricotPhase1+: the header commits the hash of the extData.
			name:            "ap1_matching_hash",
			isApricotPhase1: true,
			block:           newBlock(recordedHeight, extDataHash, extData),
		},
		{
			name:            "ap1_mismatched_hash",
			isApricotPhase1: true,
			block:           newBlock(recordedHeight, emptyExtHash, extData),
			wantErr:         errExtDataHashMismatch,
		},
		{
			// Pre-ApricotPhase1: empty header field, no recorded set.
			name:  "pre_ap1_empty_header_no_set",
			block: newBlock(unrecordedHeight, empty, nil),
		},
		{
			name:    "pre_ap1_non_empty_header",
			block:   newBlock(unrecordedHeight, extDataHash, nil),
			wantErr: errExpectedEmptyExtDataHash,
		},
		{
			// The recorded height carries extData hashing to its recorded value.
			name:   "pre_ap1_recorded_matching_extdata",
			block:  newBlock(recordedHeight, empty, extData),
			hashes: hashes,
		},
		{
			// The recorded height is unexpectedly missing its extData.
			name:    "pre_ap1_recorded_but_missing_extdata",
			block:   newBlock(recordedHeight, empty, nil),
			hashes:  hashes,
			wantErr: errUnexpectedMissingExtData,
		},
		{
			// An unrecorded height carries extData it should not have.
			name:    "pre_ap1_unrecorded_with_extdata",
			block:   newBlock(unrecordedHeight, empty, extData),
			hashes:  hashes,
			wantErr: errRecordedExtDataHashMismatch,
		},
		{
			name:   "pre_ap1_unrecorded_empty_extdata",
			block:  newBlock(unrecordedHeight, empty, nil),
			hashes: hashes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyExtDataHash(tt.isApricotPhase1, tt.block, tt.hashes)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestHashes verifies that the embedded recorded sets decode for the networks
// that have one and that other networks have none.
func TestHashes(t *testing.T) {
	require.NotEmpty(t, Hashes(constants.MainnetID), "mainnet")
	require.NotEmpty(t, Hashes(constants.FujiID), "fuji")
	require.Nil(t, Hashes(constants.LocalID), "local")
}
