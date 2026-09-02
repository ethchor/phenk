package lifecycle

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

// Job kinds and their cadences. The intervals come straight from the v0 plan:
// the point of running expiry every thirty seconds while purge runs every
// minute is that an address stops accepting mail promptly, and its contents go
// a little afterwards.
const (
	NoticeInterval  = 60 * time.Second
	ExpireInterval  = 30 * time.Second
	PurgeInterval   = 60 * time.Second
	SweepInterval   = 5 * time.Minute
	ReleaseInterval = 24 * time.Hour
	CollectInterval = 15 * time.Minute
)

// NoticeArgs warns owners that an address is about to lapse.
type NoticeArgs struct{}

func (NoticeArgs) Kind() string { return "phenk.lifecycle.notice" }

// ExpireArgs takes lapsed addresses out of service.
type ExpireArgs struct{}

func (ExpireArgs) Kind() string { return "phenk.lifecycle.expire" }

// PurgeArgs destroys the contents of expired addresses.
type PurgeArgs struct{}

func (PurgeArgs) Kind() string { return "phenk.lifecycle.purge" }

// SweepArgs applies the rolling retention of named inboxes.
type SweepArgs struct{}

func (SweepArgs) Kind() string { return "phenk.lifecycle.sweep" }

// ReleaseArgs reports on the tombstones. It deletes nothing.
type ReleaseArgs struct{}

func (ReleaseArgs) Kind() string { return "phenk.lifecycle.release" }

// CollectArgs removes blobs nothing references any more.
type CollectArgs struct{}

func (CollectArgs) Kind() string { return "phenk.lifecycle.collect" }

// worker adapts one Runner method to the queue.
type worker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	run func(context.Context) error
}

func (w *worker[T]) Work(ctx context.Context, _ *river.Job[T]) error { return w.run(ctx) }

// Register adds every lifecycle worker to a queue's worker set.
func Register(workers *river.Workers, runner *Runner) {
	river.AddWorker(workers, &worker[NoticeArgs]{run: ignoreCount(runner.Notice)})
	river.AddWorker(workers, &worker[ExpireArgs]{run: ignoreCount(runner.Expire)})
	river.AddWorker(workers, &worker[PurgeArgs]{run: ignoreCount(runner.Purge)})
	river.AddWorker(workers, &worker[SweepArgs]{run: ignoreCount(runner.Sweep)})
	river.AddWorker(workers, &worker[CollectArgs]{run: ignoreCount(runner.CollectOrphans)})
	river.AddWorker(workers, &worker[ReleaseArgs]{run: func(ctx context.Context) error {
		_, err := runner.Release(ctx)
		return err
	}})
}

// PeriodicJobs is the schedule.
//
// Every job runs on start as well as on its interval, so a process that has
// been down does not wait a full period before catching up on the expiries that
// accumulated while it was away.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
		periodic(NoticeInterval, NoticeArgs{}),
		periodic(ExpireInterval, ExpireArgs{}),
		periodic(PurgeInterval, PurgeArgs{}),
		periodic(SweepInterval, SweepArgs{}),
		periodic(CollectInterval, CollectArgs{}),
		periodic(ReleaseInterval, ReleaseArgs{}),
	}
}

func periodic(every time.Duration, args river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(every),
		func() (river.JobArgs, *river.InsertOpts) { return args, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}

// ignoreCount adapts a job that reports how much it did to the queue, which
// only cares whether it failed.
func ignoreCount(run func(context.Context) (int, error)) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := run(ctx)
		return err
	}
}
