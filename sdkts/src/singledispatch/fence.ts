// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import type { Call } from '../credentials.js';
import { CallIDSchema } from '../types.js';
import { decideDispatch, type Decision, type Marker } from './index.js';

/** The current attempt has no permission to send another provider write. */
export class DispatchNotAllowedError extends Error { constructor() { super('single dispatch is not allowed for this attempt'); this.name = 'DispatchNotAllowedError'; } }
/** The provider request cannot fit within the protected attempt budget. */
export class InsufficientTimeError extends Error { constructor() { super('insufficient attempt time for single dispatch'); this.name = 'InsufficientTimeError'; } }
/** A claim heartbeat failed before any provider request was sent. */
export class CheckpointError extends Error { constructor() { super('single-dispatch checkpoint could not be recorded'); this.name = 'CheckpointError'; } }

/** Sets the fraction of the initial attempt budget available to Dispatch. */
export interface FenceOptions {
  /** Positive fraction up to 5/6; default is 7/12. */ writeWindowFraction?: number;
}

/** One-attempt fence for a non-idempotent provider write. Construct once per Step attempt. */
export class Fence {
  readonly #context: AsyncContext;
  readonly #call: Call;
  readonly #writeDeadline: number;
  readonly #minimumRequestTime: number;
  #decision: Decision | undefined;
  #hasDispatched = false;
  #hasSendFinished = false;
  #hasPreSendRetry = false;
  #hasTerminalRecordPending = false;
  #marker: Marker | undefined;

  /** Capture the smaller of the configured Execute timeout and the public Dex attempt deadline. */
  constructor(call: Call, executeTimeoutMs: number, options: FenceOptions = {}) {
    if (!call?.context || !CallIDSchema.safeParse(call.id).success || !Number.isSafeInteger(call.context.attempt) || call.context.attempt < 1 || !Number.isFinite(executeTimeoutMs) || executeTimeoutMs <= 0) throw new TypeError('single dispatch requires a call, positive attempt, and Execute timeout');
    const fraction = options.writeWindowFraction ?? 7 / 12;
    if (!Number.isFinite(fraction) || fraction <= 0 || fraction > 5 / 6) throw new RangeError('single-dispatch write window must be within (0, 5/6]');
    this.#call = call;
    this.#context = call.context;
    const now = Date.now();
    const remaining = this.#context.deadline ? this.#context.deadline.getTime() - now : executeTimeoutMs;
    const budget = Math.max(0, Math.min(executeTimeoutMs, remaining));
    this.#writeDeadline = now + budget * fraction;
    this.#minimumRequestTime = budget / 10;
  }

  /** Decide before resolving credentials or performing any provider lookup. */
  decide(): Decision {
    if (this.#decision) return this.#decision;
    let last: unknown;
    try {
      if (this.#context.hasLastHeartbeatValue()) {
        last = this.#context.getLastHeartbeatValue();
        if (last === undefined || last === null) {
          this.#decision = { kind: 'failClosed', error: new TypeError('invalid single-dispatch checkpoint') };
          return this.#decision;
        }
      }
    } catch (error) {
      this.#decision = decideDispatch(this.#call.id, this.#context.attempt, undefined, error instanceof Error ? error : new Error('heartbeat checkpoint is unreadable'));
      return this.#decision;
    }
    this.#decision = decideDispatch(this.#call.id, this.#context.attempt, last);
    return this.#decision;
  }

  /** Record the claim before sending once; repeat it during a long request. */
  async dispatch<Result>(send: (signal: AbortSignal) => Promise<Result> | Result): Promise<Result> {
    if (this.#decision?.kind !== 'sendAllowed' || this.#hasDispatched || this.#hasPreSendRetry || typeof send !== 'function') throw new DispatchNotAllowedError();
    if (this.#minimumRequestTime <= 0 || this.#writeDeadline - Date.now() < this.#minimumRequestTime) {
      try { await this.recordNotSent(); }
      catch (error) { throw new AggregateError([new InsufficientTimeError(), error], 'single dispatch has insufficient time and checkpoint failed'); }
      throw new InsufficientTimeError();
    }
    this.#hasDispatched = true;
    const claim: Marker = { callId: this.#call.id, attempt: this.#context.attempt, state: 'claimed' };
    try { await this.#record(claim); }
    catch {
      this.#hasPreSendRetry = true;
      try { await this.#context.recordHeartbeat({ callId: this.#call.id, attempt: this.#context.attempt, state: 'notSent' }); }
      catch { /* A failed best-effort proof never grants a send in this attempt. */ }
      throw new CheckpointError();
    }
    const requestController = new AbortController();
    const onCancel = () => requestController.abort(this.#context.cancellationSignal.reason);
    if (this.#context.cancellationSignal.aborted) onCancel();
    else this.#context.cancellationSignal.addEventListener('abort', onCancel, { once: true });
    const timeout = setTimeout(() => requestController.abort(new InsufficientTimeError()), Math.max(0, this.#writeDeadline - Date.now()));
    let pendingHeartbeat: Promise<void> = Promise.resolve();
    const ticker = setInterval(() => {
      if (this.#marker?.state !== 'claimed' || this.#marker.providerId) return;
      pendingHeartbeat = pendingHeartbeat.then(async () => {
        if (this.#marker?.state === 'claimed' && !this.#marker.providerId) {
          try { await this.#context.recordHeartbeat(claim); }
          catch { /* Repetition is best effort; a failed claim cannot authorize another send. */ }
        }
      });
    }, 5000);
    try {
      return await send(requestController.signal);
    } finally {
      clearInterval(ticker);
      clearTimeout(timeout);
      this.#context.cancellationSignal.removeEventListener('abort', onCancel);
      await pendingHeartbeat;
      this.#hasSendFinished = true;
    }
  }

  /** Checkpoint a pre-send Retry, permanently forbidding Dispatch in this attempt. */
  async recordNotSent(): Promise<void> {
    if (this.#decision?.kind !== 'sendAllowed' || this.#hasDispatched || this.#hasPreSendRetry) throw new DispatchNotAllowedError();
    this.#hasPreSendRetry = true;
    await this.#record({ callId: this.#call.id, attempt: this.#context.attempt, state: 'notSent' });
  }

  /** Checkpoint positive evidence that the prior request was not processed. */
  async recordCanResend(): Promise<void> {
    if (!this.#hasSendFinished || this.#hasTerminalRecordPending || this.#marker?.state !== 'claimed' || this.#marker.providerId) throw new DispatchNotAllowedError();
    this.#hasTerminalRecordPending = true;
    try { await this.#record({ callId: this.#call.id, attempt: this.#context.attempt, state: 'canResend' }); }
    finally { this.#hasTerminalRecordPending = false; }
  }

  /** Checkpoint an accepted provider resource for read-back after a lost response. */
  async recordProviderId(providerId: string): Promise<void> {
    if (!this.#hasSendFinished || this.#hasTerminalRecordPending || this.#marker?.state !== 'claimed' || !providerId) throw new DispatchNotAllowedError();
    this.#hasTerminalRecordPending = true;
    try { await this.#record({ callId: this.#call.id, attempt: this.#context.attempt, state: 'claimed', providerId }); }
    finally { this.#hasTerminalRecordPending = false; }
  }

  async #record(marker: Marker): Promise<void> {
    await this.#context.recordHeartbeat(marker);
    this.#marker = marker;
  }
}

/** Construct one fence for the current Dex Step attempt. */
export function newFence(call: Call, executeTimeoutMs: number, options?: FenceOptions): Fence { return new Fence(call, executeTimeoutMs, options); }
