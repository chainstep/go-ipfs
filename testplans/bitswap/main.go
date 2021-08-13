package main

import (
	"context"
	"errors"
	"math/rand"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"

	bitswap "github.com/ipfs/go-bitswap"
	"github.com/ipfs/go-bitswap/network"
	block "github.com/ipfs/go-block-format"
	datastore "github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	exchange "github.com/ipfs/go-ipfs-exchange-interface"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	dht "github.com/libp2p/go-libp2p-kad-dht"
)

var (
	testcases = map[string]interface{}{
		"speed-test": run.InitializedTestCaseFn(runSpeedTest),
	}
	readyState    = sync.State("ready")
	doneState     = sync.State("done")
	listen        = "/ip4/0.0.0.0/tcp/9000"
	providerTopic = sync.NewTopic("provider", "")
	blockTopic    = sync.NewTopic("provider", "")
)

func main() {
	run.InvokeMap(testcases)
}

func runSpeedTest(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("running speed-test")
	ctx := context.Background()
	h, err := libp2p.New(ctx, libp2p.ListenAddrStrings())
	if err != nil {
		return err
	}
	kad, err := dht.New(ctx, h)
	if err != nil {
		return err
	}
	bstore := blockstore.NewBlockstore(datastore.NewMapDatastore())
	ex := bitswap.New(ctx, network.NewFromIpfsHost(h, kad), bstore)

	switch runenv.TestGroupID {
	case "providers":

		runenv.RecordMessage("running provider")
		err = runProvide(ctx, runenv, h, bstore, ex)
	case "requestors":
		runenv.RecordMessage("running requestor")
		err = runRequest(ctx, runenv, h, bstore, ex)
	default:
		runenv.RecordMessage("not part of a group")
		err = errors.New("unknown test group id")
	}
	return err
}

func runProvide(ctx context.Context, runenv *runtime.RunEnv, h host.Host, bstore blockstore.Blockstore, ex exchange.Interface) error {
	tgc := sync.MustBoundClient(ctx, runenv)
	addrs, err := h.Network().InterfaceListenAddresses()
	if err != nil {
		return err
	}
	// tell the requestors where I am reachable
	for _, a := range addrs {
		tgc.MustPublish(ctx, providerTopic, a.String())
	}
	// wait until they are ready
	tgc.MustSignalAndWait(ctx, readyState, runenv.TestInstanceCount)

	size := runenv.SizeParam("size")
	runenv.RecordMessage("generating %s-sized random block", size)
	buf := make([]byte, size)
	rand.Read(buf)
	blk := block.NewBlock(buf)
	err = bstore.Put(blk)
	if err != nil {
		return err
	}
	err = ex.HasBlock(blk)
	if err != nil {
		return err
	}
	blkcid := blk.String()
	runenv.RecordMessage("publishing block %s", blkcid)
	tgc.MustPublish(ctx, blockTopic, blkcid)
	return nil
}

func runRequest(ctx context.Context, runenv *runtime.RunEnv, h host.Host, bstore blockstore.Blockstore, ex exchange.Interface) error {
	return nil
}

// size := runenv.SizeParam("size")
// bandwidths := runenv.SizeArrayParam("bandwidths")
// var latencies []time.Duration
// lats := runenv.StringArrayParam("latencies")
// for _, l := range lats {
// 	d, err := time.ParseDuration(l)
// 	if err != nil {
// 		runenv.RecordCrash(err)
// 		panic(err)
// 	}
// 	latencies = append(latencies, d)
// }
// bandwidths = append([]uint64{0}, bandwidths...)
// latencies = append([]time.Duration{0}, latencies...)

// initCtx.MustWaitAllInstancesInitialized(ctx)
// runenv.RecordMessage("all instances running")

// switch runenv.TestGroupID {
// case "providers":
// 	runenv.RecordMessage("I'm a provider, serving a %d size file", size)
// case "requesters":
// 	runenv.RecordMessage("I'm a requester")
// }
// return nil
// }
