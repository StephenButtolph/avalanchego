// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	baseImageName = "antithesis-avalanchego"
	nodeCount     = 5

	// The latest upgrade (Helicon) is scheduled to activate partway through an
	// antithesis run so that the transition across a network upgrade is
	// exercised.
	//
	// This compose configuration is generated at image build time, potentially
	// long before the network actually starts, so the activation time cannot be
	// expressed as a delay from network start. Instead, Helicon is pinned to a
	// fixed wall-clock instant (maxActivationOffset from now) and the guest.sh
	// script written alongside the compose configuration resets each run's
	// system clock to a random offset in [minActivationOffset,
	// maxActivationOffset] before that instant. This causes Helicon to activate
	// that random duration after the network starts.
	minActivationOffset = 30 * time.Second
	maxActivationOffset = 500 * time.Second
)

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	log := tests.NewDefaultLogger("")
	network, activationTime, err := newNetwork()
	if err != nil {
		log.Fatal("failed to configure network",
			zap.Error(err),
		)
		os.Exit(1)
	}
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		log.Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
	if err := antithesis.WriteGuestScript(guestScript(activationTime)); err != nil {
		log.Fatal("failed to write guest script",
			zap.Error(err),
		)
		os.Exit(1)
	}
}

// newNetwork returns a network with all upgrades initially active except the
// latest (Helicon), which is scheduled to activate partway through the run (see
// guestScript). It also returns the wall-clock time at which Helicon is
// scheduled to activate. A custom network is required because nodes refuse to
// override the local network's upgrade schedule.
func newNetwork() (*tmpnet.Network, time.Time, error) {
	network := &tmpnet.Network{
		Owner: baseImageName,
		Nodes: tmpnet.NewNodesOrPanic(nodeCount),
	}
	genesis, err := network.DefaultGenesis()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to create genesis: %w", err)
	}
	network.Genesis = genesis

	// Schedule the latest upgrade maxActivationOffset from now. guest.sh will
	// rewind each run's clock to a random offset before this time, so the clock
	// remains at or after image build time (keeping staking certs valid) while
	// the upgrade still activates mid-run.
	upgrades := tmpnet.UpgradeConfig(maxActivationOffset)
	network.DefaultFlags, err = tmpnet.UpgradeFlags(upgrades)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to get upgrade flags: %w", err)
	}
	return network, upgrades.HeliconTime, nil
}

// guestScript returns the contents of a bash script that Antithesis executes on
// the host running docker-compose before the network starts. It sets the system
// clock to a random offset in [minActivationOffset, maxActivationOffset] before
// activationTime so that the Helicon upgrade activates that random duration
// after the network starts.
//
// The random offset is drawn from /dev/urandom so that Antithesis, which
// controls that entropy source, can explore different activation timings across
// runs.
func guestScript(activationTime time.Time) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

# Helicon activation time, in seconds since the Unix epoch. Baked in when the
# antithesis config image was generated.
activation_epoch=%d

# Bounds (in seconds) on how long after the network starts Helicon activates.
min_offset=%d
max_offset=%d

# Draw a random offset in [min_offset, max_offset] from /dev/urandom so that
# Antithesis can explore different activation timings across runs.
rand=$(od -An -N4 -tu4 < /dev/urandom | tr -d '[:space:]')
offset=$(( min_offset + rand %% (max_offset - min_offset + 1) ))

start_epoch=$(( activation_epoch - offset ))
start_time=$(date -u -d "@${start_epoch}" +"%%Y-%%m-%%d %%H:%%M:%%S")

echo "Setting system clock to ${start_time} UTC; Helicon activates in ${offset}s"

timedatectl set-time "${start_time}"
`,
		activationTime.Unix(),
		int(minActivationOffset.Seconds()),
		int(maxActivationOffset.Seconds()),
	)
}
