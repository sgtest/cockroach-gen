// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package kvserver

import (
	"time"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/kvserverbase"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/stateloader"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/tenantrate"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/txnwait"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/util/hlc"
)

// StoreTestingKnobs is a part of the context used to control parts of
// the system. The Testing*Filter functions are called at various
// points in the request pipeline if they are non-nil. These can be
// used either for synchronization (e.g. to write to a channel when a
// particular point is reached) or to change the behavior by returning
// an error (which aborts all further processing for the command).
type StoreTestingKnobs struct {
	EvalKnobs               kvserverbase.BatchEvalTestingKnobs
	IntentResolverKnobs     kvserverbase.IntentResolverTestingKnobs
	TxnWaitKnobs            txnwait.TestingKnobs
	ConsistencyTestingKnobs ConsistencyTestingKnobs
	TenantRateKnobs         tenantrate.TestingKnobs

	// TestingRequestFilter is called before evaluating each request on a
	// replica. The filter is run before the request acquires latches, so
	// blocking in the filter will not block interfering requests. If it
	// returns an error, the command will not be evaluated.
	TestingRequestFilter kvserverbase.ReplicaRequestFilter

	// TestingConcurrencyRetryFilter is called before a concurrency retry error is
	// handled and the batch is retried.
	TestingConcurrencyRetryFilter kvserverbase.ReplicaConcurrencyRetryFilter

	// TestingProposalFilter is called before proposing each command.
	TestingProposalFilter kvserverbase.ReplicaProposalFilter

	// TestingApplyFilter is called before applying the results of a
	// command on each replica. If it returns an error, the command will
	// not be applied. If it returns an error on some replicas but not
	// others, the behavior is poorly defined.
	TestingApplyFilter kvserverbase.ReplicaApplyFilter

	// TestingPostApplyFilter is called after a command is applied to
	// rocksdb but before in-memory side effects have been processed.
	// It is only called on the replica the proposed the command.
	TestingPostApplyFilter kvserverbase.ReplicaApplyFilter

	// TestingResponseFilter is called after the replica processes a
	// command in order for unittests to modify the batch response,
	// error returned to the client, or to simulate network failures.
	TestingResponseFilter kvserverbase.ReplicaResponseFilter

	// TestingRangefeedFilter is called before a replica processes a rangefeed
	// in order for unit tests to modify the request, error returned to the client
	// or data.
	TestingRangefeedFilter kvserverbase.ReplicaRangefeedFilter

	// MaxOffset, if set, overrides the server clock's MaxOffset at server
	// creation time.
	// See also DisableMaxOffsetCheck.
	MaxOffset time.Duration
	// DisableMaxOffsetCheck disables the rejection (in Store.Send) of requests
	// with the timestamp too much in the future. Normally, this rejection is a
	// good sanity check, but certain tests unfortunately insert a "message from
	// the future" into the system to advance the clock of a TestServer. We
	// should get rid of such practices once we make TestServer take a
	// ManualClock.
	DisableMaxOffsetCheck bool
	// DisableAutomaticLeaseRenewal enables turning off the background worker
	// that attempts to automatically renew expiration-based leases.
	DisableAutomaticLeaseRenewal bool
	// LeaseRequestEvent, if set, is called when replica.requestLeaseLocked() is
	// called to acquire a new lease. This can be used to assert that a request
	// triggers a lease acquisition.
	LeaseRequestEvent func(ts hlc.Timestamp, storeID roachpb.StoreID, rangeID roachpb.RangeID) *roachpb.Error
	// LeaseTransferBlockedOnExtensionEvent, if set, is called when
	// replica.TransferLease() encounters an in-progress lease extension.
	// nextLeader is the replica that we're trying to transfer the lease to.
	LeaseTransferBlockedOnExtensionEvent func(nextLeader roachpb.ReplicaDescriptor)
	// DisableGCQueue disables the GC queue.
	DisableGCQueue bool
	// DisableMergeQueue disables the merge queue.
	DisableMergeQueue bool
	// DisableReplicateQueue disables the raft log queue.
	DisableRaftLogQueue bool
	// DisableReplicaGCQueue disables the replica GC queue.
	DisableReplicaGCQueue bool
	// DisableReplicateQueue disables the replication queue.
	DisableReplicateQueue bool
	// DisableReplicaRebalancing disables rebalancing of replicas but otherwise
	// leaves the replicate queue operational.
	DisableReplicaRebalancing bool
	// DisableLoadBasedSplitting turns off LBS so no splits happen because of load.
	DisableLoadBasedSplitting bool
	// DisableSplitQueue disables the split queue.
	DisableSplitQueue bool
	// DisableTimeSeriesMaintenanceQueue disables the time series maintenance
	// queue.
	DisableTimeSeriesMaintenanceQueue bool
	// DisableRaftSnapshotQueue disables the raft snapshot queue.
	DisableRaftSnapshotQueue bool
	// DisableConsistencyQueue disables the consistency checker.
	DisableConsistencyQueue bool
	// DisableScanner disables the replica scanner.
	DisableScanner bool
	// DisableLeaderFollowsLeaseholder disables attempts to transfer raft
	// leadership when it diverges from the range's leaseholder.
	DisableLeaderFollowsLeaseholder bool
	// DisableRefreshReasonNewLeader disables refreshing pending commands when a new
	// leader is discovered.
	DisableRefreshReasonNewLeader bool
	// DisableRefreshReasonNewLeaderOrConfigChange disables refreshing pending
	// commands when a new leader is discovered or when a config change is
	// dropped.
	DisableRefreshReasonNewLeaderOrConfigChange bool
	// DisableRefreshReasonTicks disables refreshing pending commands when a
	// snapshot is applied.
	DisableRefreshReasonSnapshotApplied bool
	// DisableRefreshReasonTicks disables refreshing pending commands
	// periodically.
	DisableRefreshReasonTicks bool
	// DisableEagerReplicaRemoval prevents the Replica from destroying itself
	// when it encounters a ChangeReplicasTrigger which would remove it or when
	// a ReplicaTooOldError in a RaftMessageResponse would lead to removal.
	// This option can lead to nasty cases during shutdown where a replica will
	// spin attempting to acquire a split or merge lock on a RHS which will
	// always fail and is generally not safe but is useful for testing.
	DisableEagerReplicaRemoval bool
	// RefreshReasonTicksPeriod overrides the default period over which
	// pending commands are refreshed. The period is specified as a multiple
	// of Raft group ticks.
	RefreshReasonTicksPeriod int
	// DisableProcessRaft disables the process raft loop.
	DisableProcessRaft bool
	// DisableLastProcessedCheck disables checking on replica queue last processed times.
	DisableLastProcessedCheck bool
	// ReplicateQueueAcceptsUnsplit allows the replication queue to
	// process ranges that need to be split, for use in tests that use
	// the replication queue but disable the split queue.
	ReplicateQueueAcceptsUnsplit bool
	// SplitQueuePurgatoryChan allows a test to control the channel used to
	// trigger split queue purgatory processing.
	SplitQueuePurgatoryChan <-chan time.Time
	// SkipMinSizeCheck, if set, makes the store creation process skip the check
	// for a minimum size.
	SkipMinSizeCheck bool
	// DisableLeaseCapacityGossip disables the ability of a changing number of
	// leases to trigger the store to gossip its capacity. With this enabled,
	// only changes in the number of replicas can cause the store to gossip its
	// capacity.
	DisableLeaseCapacityGossip bool
	// SystemLogsGCPeriod is used to override the period of GC of system logs.
	SystemLogsGCPeriod time.Duration
	// SystemLogsGCGCDone is used to notify when system logs GC is done.
	SystemLogsGCGCDone chan<- struct{}
	// DontPushOnWriteIntentError will propagate a write intent error immediately
	// instead of utilizing the intent resolver to try to push the corresponding
	// transaction.
	DontPushOnWriteIntentError bool
	// DontRetryPushTxnFailures will propagate a push txn failure immediately
	// instead of utilizing the txn wait queue to wait for the transaction to
	// finish or be pushed by a higher priority contender.
	DontRetryPushTxnFailures bool
	// DontRecoverIndeterminateCommits will propagate indeterminate commit
	// errors from failed txn pushes immediately instead of utilizing the txn
	// recovery manager to recovery from the indeterminate state.
	DontRecoverIndeterminateCommits bool
	// TraceAllRaftEvents enables raft event tracing even when the current
	// vmodule would not have enabled it.
	TraceAllRaftEvents bool
	// EnableUnconditionalRefreshesInRaftReady will always set the refresh reason
	// in handleRaftReady to refreshReasonNewLeaderOrConfigChange.
	EnableUnconditionalRefreshesInRaftReady bool

	// ReceiveSnapshot is run after receiving a snapshot header but before
	// acquiring snapshot quota or doing shouldAcceptSnapshotData checks. If an
	// error is returned from the hook, it's sent as an ERROR SnapshotResponse.
	ReceiveSnapshot func(*SnapshotRequest_Header) error
	// ReplicaAddSkipLearnerRollback causes replica addition to skip the learner
	// rollback that happens when either the initial snapshot or the promotion of
	// a learner to a voter fails.
	ReplicaAddSkipLearnerRollback func() bool
	// VoterAddStopAfterLearnerSnapshot causes voter addition to return early
	// if the func returns true. Specifically, after the learner txn is successful
	// and after the LEARNER type snapshot, but before promoting it to a voter.
	// This ensures the `*Replica` will be materialized on the Store when it
	// returns.
	VoterAddStopAfterLearnerSnapshot func([]roachpb.ReplicationTarget) bool
	// NonVoterAfterInitialization is called after a newly added non-voting
	// replica receives its initial snapshot. Note that this knob _can_ be used in
	// conjunction with ReplicaSkipInitialSnapshot.
	NonVoterAfterInitialization func()
	// ReplicaSkipInitialSnapshot causes snapshots to never be sent to learners or
	// non-voters if the func returns true. Adding replicas proceeds as usual,
	// though if an added voter has no prior state which can be caught up from the
	// raft log, the result will be an voter that is unable to participate in
	// quorum.
	ReplicaSkipInitialSnapshot func() bool
	// RaftSnapshotQueueSkipReplica causes the raft snapshot queue to skip sending
	// a snapshot to a follower replica.
	RaftSnapshotQueueSkipReplica func() bool
	// VoterAddStopAfterJointConfig causes voter addition to return early if
	// the func returns true. This happens before transitioning out of a joint
	// configuration, after the joint configuration has been entered by means
	// of a first ChangeReplicas transaction. If the replication change does
	// not use joint consensus, this early return is identical to the regular
	// return path.
	VoterAddStopAfterJointConfig func() bool
	// ReplicationAlwaysUseJointConfig causes replica addition to always go
	// through a joint configuration, even when this isn't necessary (because
	// the replication change affects only one replica).
	ReplicationAlwaysUseJointConfig func() bool
	// BeforeSnapshotSSTIngestion is run just before the SSTs are ingested when
	// applying a snapshot.
	BeforeSnapshotSSTIngestion func(IncomingSnapshot, SnapshotRequest_Type, []string) error
	// BeforeRelocateOne intercepts the return values of s.relocateOne before
	// they're being put into effect.
	BeforeRelocateOne func(_ []roachpb.ReplicationChange, leaseTarget *roachpb.ReplicationTarget, _ error)
	// DontIgnoreFailureToTransferLease makes `AdminRelocateRange` return an error
	// to its client if it failed to transfer the lease to the first voting
	// replica in the set of relocation targets.
	DontIgnoreFailureToTransferLease bool
	// MaxApplicationBatchSize enforces a maximum size on application batches.
	// This can be useful for testing conditions which require commands to be
	// applied in separate batches.
	MaxApplicationBatchSize int
	// RangeFeedPushTxnsInterval overrides the default value for
	// rangefeed.Config.PushTxnsInterval.
	RangeFeedPushTxnsInterval time.Duration
	// RangeFeedPushTxnsAge overrides the default value for
	// rangefeed.Config.PushTxnsAge.
	RangeFeedPushTxnsAge time.Duration
	// AllowLeaseProposalWhenNotLeader, if set, makes the proposal buffer allow
	// lease request proposals even when the replica inserting that proposal is
	// not the Raft leader. This can be used in tests to allow a replica to
	// acquire a lease without first moving the Raft leadership to it (e.g. it
	// allows tests to expire leases by stopping the old leaseholder's liveness
	// heartbeats and then expect other replicas to take the lease without
	// worrying about Raft).
	AllowLeaseRequestProposalsWhenNotLeader bool
	// DontCloseTimestamps inhibits the propBuf's closing of timestamps. All Raft
	// commands will carry an empty closed timestamp.
	DontCloseTimestamps bool
	// AllowDangerousReplicationChanges disables safeguards
	// in execChangeReplicasTxn that prevent moving
	// to a configuration that cannot make progress.
	AllowDangerousReplicationChanges bool
	// AllowUnsynchronizedReplicationChanges allows calls to ChangeReplicas
	// even when the replicate queue is enabled. This often results in flaky
	// tests, so by default, it is prevented.
	AllowUnsynchronizedReplicationChanges bool
	// PurgeOutdatedReplicasInterceptor intercepts attempts to purge outdated
	// replicas in the store.
	PurgeOutdatedReplicasInterceptor func()
	// If set, use the given truncated state type when bootstrapping ranges.
	// This is used for testing the truncated state migration.
	TruncatedStateTypeOverride *stateloader.TruncatedStateType
	// If set, use the given version as the initial replica version when
	// bootstrapping ranges. This is used for testing the migration
	// infrastructure.
	InitialReplicaVersionOverride *roachpb.Version
	// GossipWhenCapacityDeltaExceedsFraction specifies the fraction from the last
	// gossiped store capacity values which need be exceeded before the store will
	// gossip immediately without waiting for the periodic gossip interval.
	GossipWhenCapacityDeltaExceedsFraction float64
	// TimeSeriesDataStore is an interface used by the store's time series
	// maintenance queue to dispatch individual maintenance tasks.
	TimeSeriesDataStore TimeSeriesDataStore
}

// ModuleTestingKnobs is part of the base.ModuleTestingKnobs interface.
func (*StoreTestingKnobs) ModuleTestingKnobs() {}

// NodeLivenessTestingKnobs allows tests to override some node liveness
// controls. When set, fields ultimately affect the NodeLivenessOptions used by
// the cluster.
type NodeLivenessTestingKnobs struct {
	// LivenessDuration overrides a liveness record's life time.
	LivenessDuration time.Duration
	// RenewalDuration specifies how long before the expiration a record is
	// heartbeated. If LivenessDuration is set, this should probably be set too.
	RenewalDuration time.Duration
	// StorePoolNodeLivenessFn is the function used by the StorePool to determine
	// whether a node is live or not.
	StorePoolNodeLivenessFn NodeLivenessFunc
}

var _ base.ModuleTestingKnobs = NodeLivenessTestingKnobs{}

// ModuleTestingKnobs implements the base.ModuleTestingKnobs interface.
func (NodeLivenessTestingKnobs) ModuleTestingKnobs() {}
