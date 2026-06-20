// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extdata

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

var (
	// errExtDataHashMismatch is returned for an ApricotPhase1+ block whose header
	// ExtDataHash does not equal the hash of its extData.
	errExtDataHashMismatch = errors.New("extra data hash mismatch")
	// errExpectedEmptyExtDataHash is returned for a pre-ApricotPhase1 block whose
	// header ExtDataHash is not empty.
	errExpectedEmptyExtDataHash = errors.New("expected ExtDataHash to be empty")
	// errUnexpectedMissingExtData is returned for a pre-ApricotPhase1 block that is
	// recorded in the network's set yet carries no extData.
	errUnexpectedMissingExtData = errors.New("found block with unexpected missing extra data")
	// errRecordedExtDataHashMismatch is returned for a pre-ApricotPhase1 block whose
	// extData does not hash to the value recorded for it in the network's set (a
	// block absent from the set has a recorded value of the empty hash).
	errRecordedExtDataHashMismatch = errors.New("extra data hash did not match the expected extra data hash")
)

// VerifyExtDataHash checks that ethBlock's extData is consistent with the
// commitment in its header, applying the rule that matches the block's upgrade.
//
// From ApricotPhase1 onward the header's ExtDataHash must equal the hash of the
// block's extData. Before ApricotPhase1 the header's ExtDataHash must be empty,
// and the extData is instead checked against hashes, the network's recorded set
// of pre-ApricotPhase1 extData hashes keyed by block height (see [Hashes]): a
// height present in the set must carry extData hashing to its recorded value,
// and a height absent from it must carry none. A nil hashes (a network without a
// recorded set) skips the pre-ApricotPhase1 extData check.
//
// The logic mirrors coreth's atomic block verification so the SAE C-Chain
// accepts exactly the same blocks coreth produced.
func VerifyExtDataHash(isApricotPhase1 bool, ethBlock *types.Block, hashes map[uint64]common.Hash) error {
	headerExtra := customtypes.GetHeaderExtra(ethBlock.Header())

	if !isApricotPhase1 {
		if hashes != nil {
			extData := customtypes.BlockExtData(ethBlock)
			extDataHash := customtypes.CalcExtDataHash(extData)
			height := ethBlock.NumberU64()
			// If there is no extra data, check that there is no extra data in the
			// hash map either to ensure we do not have a block that is unexpectedly
			// missing extra data.
			expectedExtDataHash, ok := hashes[height]
			if len(extData) == 0 {
				if ok {
					return fmt.Errorf("%w (%s, %d), expected extra data hash: %s", errUnexpectedMissingExtData, ethBlock.Hash(), height, expectedExtDataHash)
				}
			} else {
				// If there is extra data, check to make sure that the extra data hash
				// matches the expected extra data hash for this block.
				if extDataHash != expectedExtDataHash {
					return fmt.Errorf("%w: block (%s, %d) extra data hash %s, expected %s", errRecordedExtDataHashMismatch, ethBlock.Hash(), height, extDataHash, expectedExtDataHash)
				}
			}
		}
		if headerExtra.ExtDataHash != (common.Hash{}) {
			return fmt.Errorf("%w but got %x", errExpectedEmptyExtDataHash, headerExtra.ExtDataHash)
		}
		return nil
	}

	extData := customtypes.BlockExtData(ethBlock)
	hash := customtypes.CalcExtDataHash(extData)
	if headerExtra.ExtDataHash != hash {
		return fmt.Errorf("%w: have %x, want %x", errExtDataHashMismatch, headerExtra.ExtDataHash, hash)
	}
	return nil
}
