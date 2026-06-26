#!/usr/bin/env bash

set -euo pipefail

# Tests the activation of the latest upgrade. The network is started before the
# latest upgrade, transactions are issued, the network activates the latest
# upgrade mid-test, and further transactions are issued.
#
# e.g.,
# ./scripts/tests.e2e.transition.sh
# ./scripts/tests.e2e.transition.sh --latest-activation-delay=4m   # Extend the pre-upgrade window
# AVALANCHEGO_PATH=./build/avalanchego ./scripts/tests.e2e.transition.sh
if ! [[ "$0" =~ scripts/tests.e2e.transition.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# The transition suite starts its own network, so it must run serially. Delegate
# to the shared e2e runner to reuse its avalanchego-path defaulting and ginkgo
# invocation, targeting only the transition suite.
E2E_SERIAL="${E2E_SERIAL:-1}" E2E_TARGET="${E2E_TARGET:-./tests/e2e/transition}" \
  ./scripts/tests.e2e.sh "${@}"
