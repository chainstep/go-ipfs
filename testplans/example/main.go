package main

import (
	"context"
	"time"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	testcases = map[string]interface{}{
		"example": run.InitializedTestCaseFn(runExample),
	}
	readyState = sync.State("ready")
)

func main() {
	run.InvokeMap(testcases)
}

func runExample(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	size := runenv.SizeParam("size")
	bandwidths := runenv.SizeArrayParam("bandwidths")
	var latencies []time.Duration
	lats := runenv.StringArrayParam("latencies")
	for _, l := range lats {
		d, err := time.ParseDuration(l)
		if err != nil {
			runenv.RecordCrash(err)
			panic(err)
		}
		latencies = append(latencies, d)
	}
	bandwidths = append([]uint64{0}, bandwidths...)
	latencies = append([]time.Duration{0}, latencies...)
	ctx := context.Background()

	initCtx.MustWaitAllInstancesInitialized(ctx)
	runenv.RecordMessage("all instances running")

	switch runenv.TestGroupID {
	case "providers":
		runenv.RecordMessage("I'm a provider, serving a %d size file", size)
	case "requesters":
		runenv.RecordMessage("I'm a requester")
	}
	return nil
}
