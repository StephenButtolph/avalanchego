// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// newExtDataBlock builds a block at the given height whose header commits
// extDataHash and whose body carries extData, without recomputing the hash, so
// a test can pair any (height, extDataHash, extData) combination.
func newExtDataBlock(height uint64, extDataHash common.Hash, extData []byte) *types.Block {
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
			block:           newExtDataBlock(recordedHeight, extDataHash, extData),
		},
		{
			name:            "ap1_mismatched_hash",
			isApricotPhase1: true,
			block:           newExtDataBlock(recordedHeight, emptyExtHash, extData),
			wantErr:         errExtDataHashMismatch,
		},
		{
			// Pre-ApricotPhase1: empty header field, no recorded set.
			name:  "pre_ap1_empty_header_no_set",
			block: newExtDataBlock(unrecordedHeight, empty, nil),
		},
		{
			name:    "pre_ap1_non_empty_header",
			block:   newExtDataBlock(unrecordedHeight, extDataHash, nil),
			wantErr: errExpectedEmptyExtDataHash,
		},
		{
			// The recorded height carries extData hashing to its recorded value.
			name:   "pre_ap1_recorded_matching_extdata",
			block:  newExtDataBlock(recordedHeight, empty, extData),
			hashes: hashes,
		},
		{
			// The recorded height is unexpectedly missing its extData.
			name:    "pre_ap1_recorded_but_missing_extdata",
			block:   newExtDataBlock(recordedHeight, empty, nil),
			hashes:  hashes,
			wantErr: errUnexpectedMissingExtData,
		},
		{
			// An unrecorded height carries extData it should not have.
			name:    "pre_ap1_unrecorded_with_extdata",
			block:   newExtDataBlock(unrecordedHeight, empty, extData),
			hashes:  hashes,
			wantErr: errRecordedExtDataHashMismatch,
		},
		{
			name:   "pre_ap1_unrecorded_empty_extdata",
			block:  newExtDataBlock(unrecordedHeight, empty, nil),
			hashes: hashes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyExtDataHash(tt.isApricotPhase1, tt.block, tt.hashes)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestExtDataHashes verifies that the embedded recorded sets decode for the
// networks that have one and that other networks have none.
func TestExtDataHashes(t *testing.T) {
	require.NotEmpty(t, extDataHashes(constants.MainnetID), "mainnet")
	require.NotEmpty(t, extDataHashes(constants.FujiID), "fuji")
	require.Nil(t, extDataHashes(constants.LocalID), "local")
}
