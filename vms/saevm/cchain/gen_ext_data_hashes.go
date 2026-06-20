//go:build ignore

// gen_ext_data_hashes generates extdata-fuji.json and extdata-mainnet.json by
// scraping the C-Chain eth JSON-RPC for pre-ApricotPhase1 block extData.
//
// Run from the repository root:
//
//	go run vms/saevm/cchain/gen_ext_data_hashes.go
//
// Optional flags:
//
//	-fuji     Fuji C-Chain RPC URL     (default: public Fuji endpoint)
//	-mainnet  Mainnet C-Chain RPC URL  (default: public Mainnet endpoint)
//	-out      Output directory         (default: vms/saevm/cchain)
//
// The program iterates blocks from 0 until it encounters the first
// ApricotPhase1 block.  Post-AP1 headers always carry a non-zero ExtDataHash
// (EmptyExtDataHash = RLPHash(nil) even when there is no extData); pre-AP1
// headers always carry the zero hash.  That invariant is the stop condition —
// no hardcoded AP1 timestamp is needed.
//
// For each pre-AP1 block that carries extData the program records:
//
//	blockHeight → CalcExtDataHash(extData)   i.e. RLPHash(extData)
//
// Blocks without extData are absent from the output, matching the semantics of
// [VM.ParseBlock]: a missing entry means "expect EmptyExtDataHash".
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

const (
	defaultFujiRPC    = "https://api.avax-test.network/ext/bc/C/rpc"
	defaultMainnetRPC = "https://api.avax.network/ext/bc/C/rpc"
)

func main() {
	fujiURL    := flag.String("fuji", defaultFujiRPC, "Fuji C-Chain RPC URL")
	mainnetURL := flag.String("mainnet", defaultMainnetRPC, "Mainnet C-Chain RPC URL")
	outDir     := flag.String("out", filepath.Join("vms", "saevm", "cchain"), "output directory")
	flag.Parse()

	nets := []struct {
		name, url, file string
	}{
		{"fuji", *fujiURL, "extdata-fuji.json"},
		{"mainnet", *mainnetURL, "extdata-mainnet.json"},
	}

	for _, net := range nets {
		log.Printf("scraping %s (%s)…", net.name, net.url)
		hashes, err := scrape(context.Background(), net.url)
		if err != nil {
			log.Fatalf("%s: %v", net.name, err)
		}
		log.Printf("%s: %d pre-AP1 blocks with extData", net.name, len(hashes))

		data, err := json.MarshalIndent(hashes, "", "\t")
		if err != nil {
			log.Fatalf("marshal: %v", err)
		}
		data = append(data, '\n')

		path := filepath.Join(*outDir, net.file)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		log.Printf("wrote %s", path)
	}
}

// cBlock is the subset of the C-Chain eth_getBlockByNumber response we need.
// Coreth extends the standard Ethereum block JSON with extDataHash (from the
// header's HeaderExtra) and extData (from the body's BlockBodyExtra).
type cBlock struct {
	Number      hexutil.Uint64 `json:"number"`
	ExtDataHash common.Hash    `json:"extDataHash"`
	ExtData     hexutil.Bytes  `json:"extData"`
}

// scrape iterates blocks from height 0 until the first post-ApricotPhase1
// block and returns a map of height → CalcExtDataHash(extData) for every
// pre-AP1 block that carried non-empty extData.
func scrape(ctx context.Context, url string) (map[uint64]common.Hash, error) {
	client, err := rpc.DialContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	hashes := make(map[uint64]common.Hash)

	for height := uint64(0); ; height++ {
		var b cBlock
		if err := client.CallContext(ctx, &b, "eth_getBlockByNumber", hexutil.EncodeUint64(height), false); err != nil {
			return nil, fmt.Errorf("block %d: %w", height, err)
		}

		// Post-AP1 headers always carry a non-zero ExtDataHash (even for
		// blocks without extData, where it equals EmptyExtDataHash = RLPHash(nil)).
		// Pre-AP1 headers always carry the zero hash.
		if b.ExtDataHash != (common.Hash{}) {
			break
		}

		if len(b.ExtData) > 0 {
			hashes[height] = customtypes.CalcExtDataHash(b.ExtData)
		}

		if height%1000 == 0 && height > 0 {
			log.Printf("  … height %d, %d entries so far", height, len(hashes))
		}
	}

	return hashes, nil
}
