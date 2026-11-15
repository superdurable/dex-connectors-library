// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, newTriggerContext, triggerErrorAttributes, type TriggerContext, type TriggerLogger } from '../trigger_delivery.js';
import { ConnectionRefSchema, type ConnectionRef } from '../types.js';
import { PollWakeSignal, currentPollClock } from './clock.js';
import { attachRequestLimiter, type RequestLimiter } from './fetcher.js';
import { CursorPoller, type Poller } from './poller.js';

const defaultJitterFraction = 0.1;
const defaultMaxBackoffMs = 15 * 60 * 1000;
const defaultMaxPagesPerPoll = 10;

/** How often a runner polls and how it backs off. Durations are milliseconds. */
export interface PollSchedule {
  /** The wait between the end of one poll of a poller and the start of its next. Required, at least 1000. */ intervalMs: number;
  /**
   * Spreads each wait uniformly within ±intervalMs*jitterFraction, and delays each poller's first poll by a
   * uniform [0, intervalMs*jitterFraction]. Omitted means 0.1, zero disables jitter, and the maximum is 0.5.
   */
  jitterFraction?: number | undefined;
  /**
   * Caps the wait after consecutive failures and is the retry period of a configuration fault. Omitted or
   * zero means 15 minutes, or intervalMs when that is longer; otherwise it must be at least intervalMs.
   */
  maxBackoffMs?: number | undefined;
  /**
   * Bounds the pages one poll reads before yielding to the other pollers; a poller with more pages polls
   * again right after them. Omitted or zero means 10.
   */
  maxPagesPerPoll?: number | undefined;
}

/** A PollSchedule with every default applied. */
export interface ResolvedPollSchedule { intervalMs: number; jitterFraction: number; maxBackoffMs: number; maxPagesPerPoll: number }

/** Apply defaults and validate; throws RangeError for an invalid schedule. */
export function resolvePollSchedule(schedule: PollSchedule): ResolvedPollSchedule {
  const intervalMs = schedule?.intervalMs;
  if (typeof intervalMs !== 'number' || !Number.isFinite(intervalMs) || intervalMs < 1000) throw new RangeError('poll interval must be at least one second');
  const jitterFraction = schedule.jitterFraction ?? defaultJitterFraction;
  if (!(jitterFraction >= 0 && jitterFraction <= 0.5)) throw new RangeError('poll jitter fraction must be 0 to 0.5');
  const maxBackoffMs = schedule.maxBackoffMs || Math.max(defaultMaxBackoffMs, intervalMs);
  if (!Number.isFinite(maxBackoffMs) || !(maxBackoffMs >= intervalMs)) throw new RangeError('poll max backoff must be at least the interval');
  const maxPagesPerPoll = schedule.maxPagesPerPoll || defaultMaxPagesPerPoll;
  if (!Number.isInteger(maxPagesPerPoll) || maxPagesPerPoll < 1) throw new RangeError('poll max pages per poll must be positive');
  return { intervalMs, jitterFraction, maxBackoffMs, maxPagesPerPoll };
}

/** Configures one runner for every poller of one provider connection. */
export interface RunnerConfig {
  /** The provider connection every poller's bindings use. */ connection: ConnectionRef;
  /** The interval, jitter, and backoff shared by the pollers. */ schedule: PollSchedule;
  /** Run in this order whenever several are due. At least one is required, and each poller belongs to one runner. */ pollers: Poller[];
  /** Optional; fetchers wait on it through waitForPollRequest. */ requestLimiter?: RequestLimiter | undefined;
  /**
   * Receives poll and delivery records with binding or feed identity, never payloads, cursor tokens, or
   * snapshot values. Omitted uses defaultTriggerLogger.
   */
  logger?: TriggerLogger | undefined;
}

/**
 * Polls every poller of one connection one at a time, so polls never overlap and ordering between pollers is
 * deterministic. A rate limit on any poller pauses all of them, because provider quotas are per user or per
 * OAuth client. It deliberately is not a TriggerRunner, whose identity is one binding; use
 * newBindingTriggerSource for that shape.
 */
export interface Runner {
  /**
   * Check every stored cursor, replay every member target with replayTriggerDeliveries in poller order, and
   * then poll until context.signal aborts. The first poll of a cursor seeds it with the provider's current
   * position, so existing items are never delivered.
   *
   * Rejects with TriggerDeliveryAbortedError when the signal aborts, with a replay error, or before any poll
   * with an error caused by PollCursorDocumentInvalidError for a stored document it can never decode; it
   * never overwrites such a document. Every other failure is logged and retried: fetch, store, and target
   * failures back off per poller, a RateLimitedError pauses every poller, and a configuration fault retries
   * every maxBackoffMs. A runner runs once; a second run rejects.
   */
  run(context: TriggerContext): Promise<void>;
  /**
   * Make the poller with that binding or feed name due now. It never blocks, coalesces repeated calls,
   * ignores unknown names, and does not shorten a rate-limit pause.
   */
  wakePoller(pollerName: string): void;
}

/**
 * Validate config and attach its pollers. Throws for an empty poller list, a poller whose bindings use another
 * connection, two pollers with the same name, a binding served twice, and a poller already attached to
 * another runner. It reads and writes nothing.
 */
export function newRunner(config: RunnerConfig): Runner {
  const connection = ConnectionRefSchema.parse(config.connection);
  const schedule = resolvePollSchedule(config.schedule);
  if (!config.pollers?.length) throw new TypeError('poll runner requires at least one poller');
  const names = new Set<string>();
  const bindings = new Set<string>();
  const pollers: CursorPoller<unknown>[] = [];
  for (const poller of config.pollers) {
    if (!(poller instanceof CursorPoller)) throw new TypeError('poll runner poller must come from newBindingPoller or newFeedPoller');
    if (names.has(poller.pollerName)) throw new TypeError(`poll runner has two pollers named "${poller.pollerName}"`);
    names.add(poller.pollerName);
    for (const binding of poller.bindings()) {
      if (binding.connection.provider !== connection.provider || binding.connection.name !== connection.name) throw new TypeError(`poller "${poller.pollerName}" binding "${binding.name}" uses another connection`);
      const identity = JSON.stringify([binding.trigger.triggerName, binding.name]);
      if (bindings.has(identity)) throw new TypeError(`poll runner serves binding "${binding.name}" of Trigger "${binding.trigger.triggerName}" twice`);
      bindings.add(identity);
    }
    pollers.push(poller as CursorPoller<unknown>);
  }
  for (const poller of pollers) poller.attachRunner(config.logger, schedule.maxPagesPerPoll);
  return new PollRunner(schedule, pollers, config.requestLimiter);
}

/** The runner's per-poller schedule state. */
interface PollerSchedule { dueAtMs: number; failures: number }

class PollRunner implements Runner {
  #isRunning = false;
  readonly #wakeSignal = new PollWakeSignal();
  readonly #wakeNames = new Set<string>();

  constructor(private readonly schedule: ResolvedPollSchedule, private readonly pollers: CursorPoller<unknown>[], private readonly limiter: RequestLimiter | undefined) {}

  wakePoller(pollerName: string): void {
    this.#wakeNames.add(pollerName);
    this.#wakeSignal.notify();
  }

  async run(context: TriggerContext = newTriggerContext()): Promise<void> {
    if (this.#isRunning) throw new Error('poll runner is already running or has run');
    this.#isRunning = true;
    for (const poller of this.pollers) await poller.checkStoredCursor(context);
    for (const poller of this.pollers) {
      try { await poller.replay(context); }
      catch (error: unknown) {
        if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
        throw error;
      }
    }
    // A signal of this run's own carries its limiter through every context a fetcher derives, as a Go context value does.
    const signal = AbortSignal.any([context.signal]);
    const pollContext: TriggerContext = { signal, logAttributes: context.logAttributes, recordSkip: () => context.recordSkip() };
    attachRequestLimiter(signal, this.limiter);
    await this.#doPollLoop(pollContext);
  }

  async #doPollLoop(context: TriggerContext): Promise<void> {
    const clock = currentPollClock();
    const startedAt = clock.now();
    const schedules: PollerSchedule[] = this.pollers.map(() => ({ dueAtMs: startedAt + clock.jitter() * this.schedule.jitterFraction * this.schedule.intervalMs, failures: 0 }));
    let pausedUntilMs = 0;
    let rateLimitFailures = 0;
    for (;;) {
      if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
      this.#applyWakeUps(schedules);
      const current = clock.now();
      const nextAtMs = Math.max(pausedUntilMs, Math.min(...schedules.map(schedule => schedule.dueAtMs)));
      if (nextAtMs > current) {
        try { await clock.waitForPoll(context.signal, nextAtMs - current, this.#wakeSignal); }
        catch { throw new TriggerDeliveryAbortedError(); }
        continue;
      }
      for (const [index, poller] of this.pollers.entries()) {
        const schedule = schedules[index]!;
        if (schedule.dueAtMs > current) continue;
        const turn = new PollTurnDelay(this, schedule.failures);
        const outcome = await poller.poll(context, turn);
        const finishedAt = clock.now();
        if (outcome.kind === 'canceled') throw new TriggerDeliveryAbortedError();
        if (outcome.kind === 'succeeded' || outcome.kind === 'hasMorePages') {
          schedule.failures = 0;
          rateLimitFailures = 0;
          schedule.dueAtMs = outcome.kind === 'succeeded' ? finishedAt + this.jittered(this.schedule.intervalMs) : finishedAt;
        } else if (outcome.kind === 'failed') {
          const delayMs = turn.failureDelayMs();
          schedule.failures++;
          schedule.dueAtMs = finishedAt + delayMs;
          if (outcome.message) poller.logRecord(context, 'warn', outcome.message, { attempt: schedule.failures, delay_ms: delayMs, ...triggerErrorAttributes(outcome.error) });
        } else if (outcome.kind === 'configurationFault') {
          schedule.dueAtMs = finishedAt + this.schedule.maxBackoffMs;
          poller.logRecord(context, 'error', outcome.message ?? 'poll paused: cursor state exceeds bound', { delay_ms: this.schedule.maxBackoffMs, ...triggerErrorAttributes(outcome.error) });
        } else {
          rateLimitFailures++;
          const retryAfterMs = outcome.retryAfterMs ?? 0;
          const delayMs = retryAfterMs === 0 ? this.jittered(this.backoffMs(rateLimitFailures)) : Math.max(retryAfterMs, this.schedule.intervalMs);
          pausedUntilMs = finishedAt + delayMs;
          schedule.dueAtMs = pausedUntilMs;
          poller.logRecord(context, 'warn', 'poll rate limited; pausing connection', { delay_ms: delayMs, pollers: this.pollers.length, ...triggerErrorAttributes(outcome.error) });
          break;
        }
      }
    }
  }

  /** Make the pollers named by wakePoller due now. */
  #applyWakeUps(schedules: PollerSchedule[]): void {
    if (this.#wakeNames.size === 0) return;
    const current = currentPollClock().now();
    for (const [index, poller] of this.pollers.entries()) {
      if (this.#wakeNames.has(poller.pollerName) && schedules[index]!.dueAtMs > current) schedules[index]!.dueAtMs = current;
    }
    this.#wakeNames.clear();
  }

  /** Return min(intervalMs * 2^(failures-1), maxBackoffMs) for failures starting at 1. */
  backoffMs(failures: number): number {
    let delay = this.schedule.intervalMs;
    for (let completed = 1; completed < failures && delay < this.schedule.maxBackoffMs; completed++) delay *= 2;
    return Math.min(delay, this.schedule.maxBackoffMs);
  }

  /** Spread delayMs uniformly within ±delayMs*jitterFraction. */
  jittered(delayMs: number): number {
    if (this.schedule.jitterFraction === 0) return delayMs;
    return delayMs * (1 + this.schedule.jitterFraction * (2 * currentPollClock().jitter() - 1));
  }
}

/** One poll of one poller; the failure delay is computed once so the records and the actual wait agree. */
class PollTurnDelay {
  #delayMs: number | undefined;

  constructor(private readonly runner: PollRunner, private readonly failures: number) {}

  failureDelayMs(): number {
    this.#delayMs ??= this.runner.jittered(this.runner.backoffMs(this.failures + 1));
    return this.#delayMs;
  }
}
