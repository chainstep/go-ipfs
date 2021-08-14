package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

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
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
)

var (
	testcases = map[string]interface{}{
		"speed-test": run.InitializedTestCaseFn(runSpeedTest),
	}
	readyState    = sync.State("ready")
	doneState     = sync.State("done")
	providerTopic = sync.NewTopic("provider", &peer.AddrInfo{})
	blockTopic    = sync.NewTopic("blocks", "")
)

func main() {
	run.InvokeMap(testcases)
}

func runSpeedTest(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("running speed-test")
	ctx := context.Background()
	listen, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/3333", runenv.TestSubnet.IP))
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
	for _, a := range h.Addrs() {
		runenv.RecordMessage("listening on addr", a.String())
	}
	bstore := blockstore.NewBlockstore(datastore.NewMapDatastore())
	ex := bitswap.New(ctx, bsnet.NewFromIpfsHost(h, kad), bstore)
	// sleep a little to make sure listening is working
	time.Sleep(10 * time.Second)
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
	ai := peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}
	tgc.MustPublish(ctx, providerTopic, &ai)
	tgc.MustSignalAndWait(ctx, readyState, runenv.TestInstanceCount)

	size := runenv.SizeParam("size")
	count := runenv.IntParam("count")
	for i := 0; i <= count; i++ {
		runenv.RecordMessage("generating %d-sized random block", size)
		buf := make([]byte, size)
		rand.Read(buf)
		blk := block.NewBlock(buf)
		err := bstore.Put(blk)
		if err != nil {
			return err
		}
		err = ex.HasBlock(blk)
		if err != nil {
			return err
		}
		blkcid := blk.Cid().String()
		runenv.RecordMessage("publishing block %s", blkcid)
		tgc.MustPublish(ctx, blockTopic, blkcid)
	}
	tgc.MustSignalAndWait(ctx, doneState, runenv.TestInstanceCount)
	return nil
}

func runRequest(ctx context.Context, runenv *runtime.RunEnv, h host.Host, bstore blockstore.Blockstore, ex exchange.Interface) error {
	tgc := sync.MustBoundClient(ctx, runenv)
	providers := make(chan *peer.AddrInfo)
	blkcids := make(chan string)
	providerSub, err := tgc.Subscribe(ctx, providerTopic, providers)
	if err != nil {
		return err
	}
	ai := <-providers
	if err != nil {
		return err
	}

	runenv.RecordMessage("here connecting to this: %s", fmt.Sprintln(*ai))
	providerSub.Done()

	err = h.Connect(ctx, *ai)
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

	begin := time.Now()
	count := runenv.IntParam("count")
	for i := 0; i <= count; i++ {
		blkcid := <-blkcids
		runenv.RecordMessage("downloading block %s", blkcid)
		dec, err := multihash.Decode([]byte(blkcid))
		if err != nil {
			return err
		}
		mh, err := multihash.Cast(dec.Digest)
		if err != nil {
			return err
		}
		if err != nil {
			return err
		}
		dlBegin := time.Now()
		blk, err := ex.GetBlock(ctx, cid.NewCidV0(mh))
		if err != nil {
			return err
		}
		dlDuration := time.Since(dlBegin)
		runenv.RecordMessage("downloaded block %s", blk.Cid().String())
		runenv.RecordMessage("download time", dlDuration)
	}
	duration := time.Since(begin)
	runenv.RecordMessage("total time", duration)
	tgc.MustSignalEntry(ctx, doneState)
	return nil
}
