package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"

	bitswap "github.com/ipfs/go-bitswap"
	bsnet "github.com/ipfs/go-bitswap/network"
	block "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	datastore "github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	exchange "github.com/ipfs/go-ipfs-exchange-interface"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	ma "github.com/multiformats/go-multiaddr"
)

var (
	testcases = map[string]interface{}{
		"speed-test": run.InitializedTestCaseFn(runSpeedTest),
	}
	readyState    = sync.State("ready")
	doneState     = sync.State("done")
	providerTopic = sync.NewTopic("provider", &peer.AddrInfo{})
	blockTopic    = sync.NewTopic("blocks", &cid.Cid{})
	listen        ma.Multiaddr
)

func main() {
	run.InvokeMap(testcases)
}

func runSpeedTest(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("running speed-test")
	ctx := context.Background()
	var err error
	listen, err = ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3333", runenv.TestSubnet.IP))
	if err != nil {
		return err
	}
	h, err := libp2p.New(ctx, libp2p.ListenAddrs(listen))
	if err != nil {
		return err
	}
	kad, err := dht.New(ctx, h)
	if err != nil {
		return err
	}
	bstore := blockstore.NewBlockstore(datastore.NewMapDatastore())
	ex := bitswap.New(ctx, bsnet.NewFromIpfsHost(h, kad), bstore)

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
	ai, err := peer.AddrInfoFromP2pAddr(listen)
	tgc.MustPublish(ctx, providerTopic, ai)
	tgc.MustSignalAndWait(ctx, readyState, runenv.TestInstanceCount)

	size := runenv.SizeParam("size")
	runenv.RecordMessage("generating %d-sized random block", size)
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
	blkcid := blk.Cid()
	runenv.RecordMessage("publishing block %s", blkcid.String())
	tgc.MustPublish(ctx, blockTopic, &blkcid)
	return nil
}

func runRequest(ctx context.Context, runenv *runtime.RunEnv, h host.Host, bstore blockstore.Blockstore, ex exchange.Interface) error {
	tgc := sync.MustBoundClient(ctx, runenv)
	providers := make(chan *peer.AddrInfo)
	blkcids := make(chan *cid.Cid)
	providerSub, err := tgc.Subscribe(ctx, providerTopic, providers)
	if err != nil {
		return err
	}
	provider := <-providers

	providerSub.Done()
	runenv.RecordMessage("will contact the provider at %s", provider.String())

	err = h.Connect(ctx, *provider)
	if err != nil {
		return err
	}

	blockcidSub, err := tgc.Subscribe(ctx, blockTopic, blkcids)
	if err != nil {
		return err
	}
	defer blockcidSub.Done()

	// tell the provider that we're ready to go
	tgc.MustSignalAndWait(ctx, readyState, runenv.TestInstanceCount)

	for blkcid := range blkcids {
		runenv.RecordMessage("downloading block %s", blkcid.String())
		blk, err := ex.GetBlock(ctx, *blkcid)
		if err != nil {
			runenv.RecordFailure(err)
		}
		runenv.RecordMessage("downloaded block %s", blk.Cid().String())
	}
	return nil
}
