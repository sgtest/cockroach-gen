// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package colexec

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach/pkg/col/coldata"
	"github.com/cockroachdb/cockroach/pkg/col/coldatatestutils"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/colexectestutils"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecerror"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecop"
	"github.com/cockroachdb/cockroach/pkg/sql/colmem"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/testutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
)

func TestParallelUnorderedSynchronizer(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	const (
		maxInputs  = 16
		maxBatches = 16
	)

	var (
		rng, _     = randutil.NewPseudoRand()
		typs       = []*types.T{types.Int}
		numInputs  = rng.Intn(maxInputs) + 1
		numBatches = rng.Intn(maxBatches) + 1
	)

	inputs := make([]SynchronizerInput, numInputs)
	for i := range inputs {
		source := colexecop.NewRepeatableBatchSource(
			testAllocator,
			coldatatestutils.RandomBatch(testAllocator, rng, typs, coldata.BatchSize(), 0 /* length */, rng.Float64()),
			typs,
		)
		source.ResetBatchesToReturn(numBatches)
		inputs[i].Op = source
		inputIdx := i
		inputs[i].MetadataSources = []colexecop.MetadataSource{
			colexectestutils.CallbackMetadataSource{DrainMetaCb: func(_ context.Context) []execinfrapb.ProducerMetadata {
				return []execinfrapb.ProducerMetadata{{Err: errors.Errorf("input %d test-induced metadata", inputIdx)}}
			}},
		}
	}

	ctx, cancelFn := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	s := NewParallelUnorderedSynchronizer(inputs, &wg)
	s.Init()

	type synchronizerTerminationScenario int
	const (
		// synchronizerGracefulTermination is a termination scenario where the
		// synchronizer terminates gracefully.
		synchronizerGracefulTermination synchronizerTerminationScenario = iota
		// synchronizerContextCanceled is a termination scenario where a
		// cancellation requests that a synchronizer terminates.
		synchronizerContextCanceled
		// synchronizerPrematureDrainMeta is a termination scenario where DrainMeta
		// is called prematurely on the synchronizer.
		synchronizerPrematureDrainMeta
		// synchronizerMaxTerminationScenario should be at the end of the
		// termination scenario list so that it can be used as an upper bound to
		// generate any other termination scenario.
		synchronizerMaxTerminationScenario
	)
	terminationScenario := synchronizerTerminationScenario(rng.Intn(int(synchronizerMaxTerminationScenario)))

	t.Run(fmt.Sprintf("numInputs=%d/numBatches=%d/terminationScenario=%d", numInputs, numBatches, terminationScenario), func(t *testing.T) {
		if terminationScenario == synchronizerContextCanceled {
			wg.Add(1)
			sleepTime := time.Duration(rng.Intn(500)) * time.Microsecond
			go func() {
				time.Sleep(sleepTime)
				cancelFn()
				wg.Done()
			}()
		} else {
			// Appease the linter complaining about context leaks.
			defer cancelFn()
		}

		batchesReturned := 0
		expectedBatchesReturned := numInputs * numBatches
		for {
			expectZeroBatch := false
			if terminationScenario == synchronizerPrematureDrainMeta && batchesReturned < expectedBatchesReturned {
				// Call DrainMeta before the input is finished. Intentionally allow
				// for Next to be called even though it's not technically supported to
				// ensure that a zero-length batch is returned.
				meta := s.DrainMeta(ctx)
				require.Equal(t, len(inputs), len(meta), "metadata length mismatch, returned metadata is: %v", meta)
				expectZeroBatch = true
			}
			var b coldata.Batch
			if err := colexecerror.CatchVectorizedRuntimeError(func() { b = s.Next(ctx) }); err != nil {
				if terminationScenario == synchronizerContextCanceled {
					require.True(t, testutils.IsError(err, "context canceled"), err)
					break
				} else {
					t.Fatal(err)
				}
			}
			if b.Length() == 0 {
				if terminationScenario == synchronizerGracefulTermination {
					// Successful run, check that all inputs have returned metadata.
					meta := s.DrainMeta(ctx)
					require.Equal(t, len(inputs), len(meta), "metadata length mismatch, returned metadata is: %v", meta)
				}
				break
			}
			if expectZeroBatch {
				t.Fatal("expected a zero batch to be returned after prematurely calling DrainMeta but that did not happen")
			}
			batchesReturned++
		}
		if terminationScenario != synchronizerContextCanceled && terminationScenario != synchronizerPrematureDrainMeta {
			require.Equal(t, expectedBatchesReturned, batchesReturned)
		}
		wg.Wait()
	})
}

func TestUnorderedSynchronizerNoLeaksOnError(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	const expectedErr = "first input error"
	ctx := context.Background()

	inputs := make([]SynchronizerInput, 6)
	inputs[0].Op = &colexecop.CallbackOperator{NextCb: func(context.Context) coldata.Batch {
		colexecerror.InternalError(errors.New(expectedErr))
		// This code is unreachable, but the compiler cannot infer that.
		return nil
	}}
	for i := 1; i < len(inputs); i++ {
		acc := testMemMonitor.MakeBoundAccount()
		defer acc.Close(ctx)
		func(allocator *colmem.Allocator) {
			inputs[i].Op = &colexecop.CallbackOperator{
				NextCb: func(ctx context.Context) coldata.Batch {
					// All inputs that do not encounter an error will continue to return
					// batches.
					b := allocator.NewMemBatchWithMaxCapacity([]*types.T{types.Int})
					b.SetLength(1)
					return b
				},
			}
		}(
			// Make a separate allocator per input, since each input will call Next in
			// a different goroutine.
			colmem.NewAllocator(ctx, &acc, coldata.StandardColumnFactory),
		)
	}

	var wg sync.WaitGroup
	s := NewParallelUnorderedSynchronizer(inputs, &wg)
	s.Init()
	for {
		if err := colexecerror.CatchVectorizedRuntimeError(func() { _ = s.Next(ctx) }); err != nil {
			require.True(t, testutils.IsError(err, expectedErr), err)
			break
		}
		// Loop until we get an error.
	}
	// The caller must call DrainMeta on error.
	require.Zero(t, len(s.DrainMeta(ctx)))
	// This is the crux of the test: assert that all inputs have finished.
	require.Equal(t, len(inputs), int(atomic.LoadUint32(&s.numFinishedInputs)))
}

func BenchmarkParallelUnorderedSynchronizer(b *testing.B) {
	defer log.Scope(b).Close(b)
	const numInputs = 6

	typs := []*types.T{types.Int}
	inputs := make([]SynchronizerInput, numInputs)
	for i := range inputs {
		batch := testAllocator.NewMemBatchWithMaxCapacity(typs)
		batch.SetLength(coldata.BatchSize())
		inputs[i].Op = colexecop.NewRepeatableBatchSource(testAllocator, batch, typs)
	}
	var wg sync.WaitGroup
	ctx, cancelFn := context.WithCancel(context.Background())
	s := NewParallelUnorderedSynchronizer(inputs, &wg)
	s.Init()
	b.SetBytes(8 * int64(coldata.BatchSize()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Next(ctx)
	}
	b.StopTimer()
	cancelFn()
	wg.Wait()
}
