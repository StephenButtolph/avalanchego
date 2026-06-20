// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extdata records the extData hashes of pre-ApricotPhase1 C-Chain
// blocks and verifies a block's extData against the commitment in its header.
//
// Before ApricotPhase1, C-Chain blocks left the header's ExtDataHash field
// empty even when they carried atomic-transaction extData, so the header alone
// does not commit to that extData. The integrity of those blocks is instead
// pinned by [Hashes], a per-network record of their extData hashes keyed by
// block height and embedded into the binary. From ApricotPhase1 onward the
// header commits to the extData directly and no recorded set is needed.
//
// This data and the [VerifyExtDataHash] rules are copied from coreth so the SAE
// C-Chain can parse the full block history without depending on coreth. Coreth
// keys the same data by block hash; the SAE C-Chain keys it by height, which is
// equally sufficient (each height has one canonical block) and far smaller.
package extdata

import (
	"encoding/json"
	"sync"

	"github.com/ava-labs/libevm/common"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	//go:embed fuji.json
	rawFujiHashes  []byte
	fujiHashesOnce sync.Once
	fujiHashes     map[uint64]common.Hash

	//go:embed mainnet.json
	rawMainnetHashes  []byte
	mainnetHashesOnce sync.Once
	mainnetHashes     map[uint64]common.Hash
)

// Hashes returns the recorded extData hashes of pre-ApricotPhase1 blocks for the
// given network, keyed by block height, or nil for networks without a recorded
// set (any network other than mainnet and fuji). The set is decoded from the
// embedded data on first use and cached, so a network that never requests it
// pays neither the decode nor the memory cost.
func Hashes(networkID uint32) map[uint64]common.Hash {
	switch networkID {
	case constants.MainnetID:
		mainnetHashesOnce.Do(func() {
			mainnetHashes = decode(rawMainnetHashes)
		})
		return mainnetHashes
	case constants.FujiID:
		fujiHashesOnce.Do(func() {
			fujiHashes = decode(rawFujiHashes)
		})
		return fujiHashes
	default:
		return nil
	}
}

// decode unmarshals embedded hash data. The data is a compile-time constant, so
// a failure here is a build defect rather than a runtime condition.
func decode(raw []byte) map[uint64]common.Hash {
	hashes := make(map[uint64]common.Hash)
	if err := json.Unmarshal(raw, &hashes); err != nil {
		panic(err)
	}
	return hashes
}
