// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// TestExtDataHashes verifies that the embedded recorded sets decode for the
// networks that have one and that other networks have none.
func TestExtDataHashes(t *testing.T) {
	require.NotEmpty(t, extDataHashes[constants.MainnetID], "mainnet")
	require.NotEmpty(t, extDataHashes[constants.FujiID], "fuji")
	require.Nil(t, extDataHashes[constants.LocalID], "local")
}
