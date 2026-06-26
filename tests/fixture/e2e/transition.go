// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

// This file provides a small, reusable framework for exercising a live network
// fork transition in an e2e test. The network is started with every fork prior
// to a target fork active at genesis, and the target fork scheduled to activate
// a short, configurable interval after the network is configured. A test can
// then exercise behavior before the transition, wait for the transition to
// occur, and exercise behavior after it.
//
// The only transition exercised today is the C-Chain's swap to the latest
// upgrade (see tests/e2e/transition), but the helpers below are fork-agnostic
// and intended to be reused for future upgrades.

// DefaultTransitionActivationDelay is the default interval after which the
// target fork is scheduled to activate. It must comfortably exceed the time it
// takes a network to bootstrap so that the pre-transition phase of a test has
// time to run before the fork activates.
const DefaultTransitionActivationDelay = 3 * time.Minute

// NewTransitionUpgradeConfig returns an upgrade config that activates every fork
// prior to targetFork at genesis and schedules targetFork to activate at
// activationTime. All forks after targetFork remain unscheduled.
func NewTransitionUpgradeConfig(targetFork upgradetest.Fork, activationTime time.Time) upgrade.Config {
	// Schedule targetFork (and, transiently, all prior forks) at activationTime.
	upgrades := upgradetest.GetConfigWithUpgradeTime(targetFork, activationTime)
	// Pull every fork before the target back to genesis so the network starts one
	// fork behind the target.
	upgradetest.SetTimesTo(&upgrades, targetFork-1, upgrade.InitiallyActiveTime)
	// Use a short epoch duration so proposervm epochs advance quickly during the
	// test rather than waiting for the production default.
	upgrades.GraniteEpochDuration = 4 * time.Second
	return upgrades
}

// NewTransitionUpgradeFlags builds the tmpnet flags needed to start a network
// configured to transition to targetFork activationDelay from now. The absolute
// activation time is returned so the caller can coordinate the test with the
// scheduled transition.
func NewTransitionUpgradeFlags(
	tc tests.TestContext,
	targetFork upgradetest.Fork,
	activationDelay time.Duration,
) (tmpnet.FlagsMap, time.Time) {
	activationTime := time.Now().Add(activationDelay)
	upgrades := NewTransitionUpgradeConfig(targetFork, activationTime)

	tc.Log().Info("configuring network for fork transition",
		zap.Stringer("targetFork", targetFork),
		zap.Time("activationTime", activationTime),
		zap.Duration("activationDelay", activationDelay),
	)

	upgradeJSON, err := json.Marshal(upgrades)
	require.NoError(tc, err)

	return tmpnet.FlagsMap{
		config.UpgradeFileContentKey: base64.StdEncoding.EncodeToString(upgradeJSON),
	}, activationTime
}
