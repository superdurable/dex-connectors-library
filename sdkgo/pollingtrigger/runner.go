// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
)

const (
	defaultJitterFraction  = 0.1
	defaultMaxBackoff      = 15 * time.Minute
	defaultMaxPagesPerPoll = 10
)

// Injected for tests, like sdkgo's Trigger retry wait.
var (
	now          = time.Now
	jitterSource = rand.Float64
	// waitForPoll waits for delay or a wake-up and returns ctx.Err() as soon as ctx ends.
	waitForPoll = func(ctx context.Context, delay time.Duration, wake <-chan struct{}) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
			return nil
		case <-timer.C:
			return nil
		}
	}
)

// PollSchedule sets how often a runner polls and how it backs off.
type PollSchedule struct {
	// Interval is the wait between the end of one poll of a poller and the start of its next. Required,
	// at least one second.
	Interval time.Duration
	// JitterFraction spreads each wait uniformly within ±Interval*JitterFraction, and delays each
	// poller's first poll by a uniform [0, Interval*JitterFraction]. Nil means 0.1, zero disables jitter,
	// and the maximum is 0.5.
	JitterFraction *float64
	// MaxBackoff caps the wait after consecutive failures and is the retry period of a configuration
	// fault. Zero means 15 minutes, or Interval when that is longer; otherwise it must be at least Interval.
	MaxBackoff time.Duration
	// MaxPagesPerPoll bounds the pages one poll reads before yielding to the other pollers; a poller with
	// more pages polls again right after them. Zero means 10.
	MaxPagesPerPoll int
}

func resolvePollSchedule(schedule PollSchedule) (PollSchedule, error) {
	if schedule.Interval < time.Second {
		return schedule, errors.New("poll interval must be at least one second")
	}
	if schedule.JitterFraction == nil {
		fraction := defaultJitterFraction
		schedule.JitterFraction = &fraction
	}
	if *schedule.JitterFraction < 0 || *schedule.JitterFraction > 0.5 {
		return schedule, errors.New("poll jitter fraction must be 0 to 0.5")
	}
	if schedule.MaxBackoff == 0 {
		schedule.MaxBackoff = max(defaultMaxBackoff, schedule.Interval)
	}
	if schedule.MaxBackoff < schedule.Interval {
		return schedule, errors.New("poll max backoff must be at least the interval")
	}
	if schedule.MaxPagesPerPoll == 0 {
		schedule.MaxPagesPerPoll = defaultMaxPagesPerPoll
	}
	if schedule.MaxPagesPerPoll < 1 {
		return schedule, errors.New("poll max pages per poll must be positive")
	}
	return schedule, nil
}

// RunnerConfig configures one runner for every poller of one provider connection.
type RunnerConfig struct {
	// Connection is the provider connection every poller's bindings use.
	Connection sdkgo.ConnectionRef
	// Schedule sets the interval, jitter, and backoff shared by the pollers.
	Schedule PollSchedule
	// Pollers run in this order whenever several are due. At least one is required, and each poller
	// belongs to one runner.
	Pollers []Poller
	// RequestLimiter is optional; fetchers wait on it through WaitForPollRequest.
	RequestLimiter RequestLimiter
	// Logger receives poll and delivery records with binding or feed identity, never payloads, cursor
	// tokens, or snapshot values. Nil uses slog.Default() as of each record.
	Logger *slog.Logger
}

// Runner polls every poller of one connection from one goroutine, so polls never overlap and ordering
// between pollers is deterministic. A rate limit on any poller pauses all of them, because provider
// quotas are per user or per OAuth client. Runner deliberately does not implement sdkgo.TriggerRunner,
// whose identity is one binding; use NewBindingTriggerSource for that shape.
type Runner struct {
	connection sdkgo.ConnectionRef
	schedule   PollSchedule
	pollers    []Poller
	limiter    RequestLimiter
	isRunning  atomic.Bool
	wakeSignal chan struct{}
	wakeMutex  sync.Mutex
	wakeNames  map[string]bool
}

// NewRunner validates config and attaches its pollers. It rejects an empty poller list, a poller whose
// bindings use another connection, two pollers with the same name, a binding served twice, and a poller
// already attached to another runner. It reads and writes nothing.
func NewRunner(config RunnerConfig) (*Runner, error) {
	if err := config.Connection.Validate(); err != nil {
		return nil, fmt.Errorf("poll runner connection: %w", err)
	}
	schedule, err := resolvePollSchedule(config.Schedule)
	if err != nil {
		return nil, err
	}
	if len(config.Pollers) == 0 {
		return nil, errors.New("poll runner requires at least one poller")
	}
	names := map[string]bool{}
	bindings := map[[2]string]bool{}
	for _, poller := range config.Pollers {
		if poller == nil {
			return nil, errors.New("poll runner poller is nil")
		}
		if names[poller.pollerName()] {
			return nil, fmt.Errorf("poll runner has two pollers named %q", poller.pollerName())
		}
		names[poller.pollerName()] = true
		for _, binding := range poller.pollerBindings() {
			if binding.Connection != config.Connection {
				return nil, fmt.Errorf("poller %q binding %q uses another connection", poller.pollerName(), binding.Name)
			}
			identity := [2]string{binding.Trigger.TriggerName, binding.Name}
			if bindings[identity] {
				return nil, fmt.Errorf("poll runner serves binding %q of Trigger %q twice", binding.Name, binding.Trigger.TriggerName)
			}
			bindings[identity] = true
		}
	}
	for _, poller := range config.Pollers {
		if err := poller.attachRunner(config.Logger, schedule.MaxPagesPerPoll); err != nil {
			return nil, err
		}
	}
	return &Runner{
		connection: config.Connection, schedule: schedule, pollers: config.Pollers, limiter: config.RequestLimiter,
		wakeSignal: make(chan struct{}, 1), wakeNames: map[string]bool{},
	}, nil
}

// WakePoller makes the poller with that binding or feed name due now. It never blocks, coalesces
// repeated calls, ignores unknown names, and does not shorten a rate-limit pause. It is safe to call from
// any goroutine, for example a provider push notification handler.
func (runner *Runner) WakePoller(pollerName string) {
	runner.wakeMutex.Lock()
	runner.wakeNames[pollerName] = true
	runner.wakeMutex.Unlock()
	select {
	case runner.wakeSignal <- struct{}{}:
	default:
	}
}

// pollerSchedule is the runner's per-poller schedule state.
type pollerSchedule struct {
	dueAt    time.Time
	failures int
}

// pollTurn is one poll of one poller. failureDelay is the wait if the poll fails, computed once so the
// records written during the poll and the actual wait agree.
type pollTurn struct {
	runner   *Runner
	failures int
	delay    *time.Duration
}

func (turn *pollTurn) failureDelay() time.Duration {
	if turn.delay == nil {
		delay := turn.runner.jittered(turn.runner.backoff(turn.failures + 1))
		turn.delay = &delay
	}
	return *turn.delay
}

// Run checks every stored cursor, replays every member target that implements
// sdkgo.TriggerDeliveryReplayer in poller order, and then polls until ctx ends. The first poll of a
// cursor seeds it with the provider's current position, so existing items are never delivered.
//
// Run returns ctx.Err() on cancellation, a replay error, or before any poll an error wrapping
// projectconfig.ErrPollCursorDocumentInvalid for a stored document it can never decode; it never
// overwrites such a document. Every other failure is logged and retried: fetch, store, and target
// failures back off per poller, a RateLimitedError pauses every poller, and a configuration fault
// retries every MaxBackoff. A runner runs once; a second Run returns an error.
func (runner *Runner) Run(ctx context.Context) error {
	if !runner.isRunning.CompareAndSwap(false, true) {
		return errors.New("poll runner is already running or has run")
	}
	for _, poller := range runner.pollers {
		if err := poller.checkStoredCursor(ctx); err != nil {
			return err
		}
	}
	for _, poller := range runner.pollers {
		if err := poller.replay(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	return runner.doPollLoop(contextWithRequestLimiter(ctx, runner.limiter))
}

func (runner *Runner) doPollLoop(ctx context.Context) error {
	schedules := make([]pollerSchedule, len(runner.pollers))
	startedAt := now()
	for index := range schedules {
		schedules[index].dueAt = startedAt.Add(time.Duration(jitterSource() * *runner.schedule.JitterFraction * float64(runner.schedule.Interval)))
	}
	var pausedUntil time.Time
	rateLimitFailures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		runner.applyWakeUps(schedules)
		current := now()
		nextAt := schedules[0].dueAt
		for _, schedule := range schedules[1:] {
			if schedule.dueAt.Before(nextAt) {
				nextAt = schedule.dueAt
			}
		}
		if pausedUntil.After(nextAt) {
			nextAt = pausedUntil
		}
		if nextAt.After(current) {
			if err := waitForPoll(ctx, nextAt.Sub(current), runner.wakeSignal); err != nil {
				return err
			}
			continue
		}
		for index, poller := range runner.pollers {
			schedule := &schedules[index]
			if schedule.dueAt.After(current) {
				continue
			}
			turn := &pollTurn{runner: runner, failures: schedule.failures}
			outcome := poller.poll(ctx, turn)
			finishedAt := now()
			switch outcome.kind {
			case pollCanceled:
				return ctx.Err()
			case pollSucceeded, pollHasMorePages:
				schedule.failures, rateLimitFailures = 0, 0
				schedule.dueAt = finishedAt
				if outcome.kind == pollSucceeded {
					schedule.dueAt = finishedAt.Add(runner.jittered(runner.schedule.Interval))
				}
			case pollFailed:
				delay := turn.failureDelay()
				schedule.failures++
				schedule.dueAt = finishedAt.Add(delay)
				if outcome.message != "" {
					poller.logRecord(ctx, slog.LevelWarn, outcome.message,
						slog.Int("attempt", schedule.failures), slog.Duration("delay", delay), triggerlog.Err(outcome.err))
				}
			case pollConfigurationFault:
				schedule.dueAt = finishedAt.Add(runner.schedule.MaxBackoff)
				poller.logRecord(ctx, slog.LevelError, outcome.message,
					slog.Duration("delay", runner.schedule.MaxBackoff), triggerlog.Err(outcome.err))
			case pollRateLimited:
				rateLimitFailures++
				delay := max(outcome.retryAfter, runner.schedule.Interval)
				if outcome.retryAfter == 0 {
					delay = runner.jittered(runner.backoff(rateLimitFailures))
				}
				pausedUntil = finishedAt.Add(delay)
				schedule.dueAt = pausedUntil
				poller.logRecord(ctx, slog.LevelWarn, "poll rate limited; pausing connection",
					slog.Duration("delay", delay), slog.Int("pollers", len(runner.pollers)), triggerlog.Err(outcome.err))
			}
			if outcome.kind == pollRateLimited {
				break
			}
		}
	}
}

// applyWakeUps makes the pollers named by WakePoller due now.
func (runner *Runner) applyWakeUps(schedules []pollerSchedule) {
	runner.wakeMutex.Lock()
	defer runner.wakeMutex.Unlock()
	if len(runner.wakeNames) == 0 {
		return
	}
	current := now()
	for index, poller := range runner.pollers {
		if runner.wakeNames[poller.pollerName()] && schedules[index].dueAt.After(current) {
			schedules[index].dueAt = current
		}
	}
	clear(runner.wakeNames)
}

// backoff returns min(Interval * 2^(failures-1), MaxBackoff) for failures starting at 1.
func (runner *Runner) backoff(failures int) time.Duration {
	delay := runner.schedule.Interval
	for completed := 1; completed < failures && delay < runner.schedule.MaxBackoff; completed++ {
		delay *= 2
	}
	return min(delay, runner.schedule.MaxBackoff)
}

// jittered spreads delay uniformly within ±delay*JitterFraction.
func (runner *Runner) jittered(delay time.Duration) time.Duration {
	fraction := *runner.schedule.JitterFraction
	if fraction == 0 {
		return delay
	}
	return time.Duration(float64(delay) * (1 + fraction*(2*jitterSource()-1)))
}
