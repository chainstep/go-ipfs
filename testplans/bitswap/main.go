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
	tgc := sync.MustBoundClient(ctx, runenv)
	blkcids := make(chan string)
	providers := make(chan string)
	providerSub, err := tgc.Subscribe(ctx, providerTopic, providers)
	if err != nil {
		return err
	}
	provider := <-providers
	providerSub.Done()
	blockcidSub, err := tgc.Subscribe(ctx, blockTopic, blkcids)
	defer blockcidSub.Done()
	runenv.RecordMessage("will contact the provider at %s", provider)
	// tell the provider that we're ready to go
	tgc.MustSignalAndWait(ctx, readyState, runenv.TestInstanceCount)

	for blkcid := range blkcids {
		runenv.RecordMessage("downloading block %s", blkcid)
	}
	return nil
}
