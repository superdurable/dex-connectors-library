// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { KeyedSnapshotEntry } from '../projectconfig/poll_cursor.js';
import { TriggerBindingRefSchema, TriggerDefinitionSchema, type TriggerBindingRef, type TriggerDefinition } from '../trigger.js';
import {
  TriggerDeliveryAbortedError, isTriggerUndeliverable, logTriggerRecord, prepareTriggerDelivery, throwIfTriggerAborted, triggerAttempt, triggerErrorAttributes, withTriggerLoggerAttributes,
  type TriggerContext, type TriggerEvent, type TriggerLogAttributes, type TriggerLogger, type TriggerTarget,
} from '../trigger_delivery.js';

/**
 * Maps one provider change to zero or more of a member's Trigger events. previous is the change's entry in the
 * feed snapshot committed before this page, or undefined when the change does not implement KeyedItem, the
 * feed keeps no snapshot, or the key is untracked. A selector must be deterministic and side-effect free: a
 * page is selected again when it is refetched after a failure.
 */
export type FeedEventSelector<Change, Payload> = (change: Change, previous: KeyedSnapshotEntry | undefined) => TriggerEvent<Payload>[];

/** One Trigger binding's view of a shared provider change feed. It is sealed: build it with newFeedMember. */
export interface FeedMember<Change> {
  /** The configured Trigger binding this member serves. */ readonly binding: TriggerBindingRef;
  /** Never set; it keeps a member's change type in its signature. */ readonly changeType?: (change: Change) => void;
}

/** The poll being processed; failureDelayMs is the wait if it fails, computed once so records and wait agree. */
export interface PollTurn { failureDelayMs(): number }

/** When a member's events reach its target relative to the cursor commit. */
export type MemberDeliveryMode =
  /** Record events in a durable target, commit, then deliver. */ | 'recordBeforeCommit'
  /** Consume events in a non-durable target before the commit. */ | 'deliverBeforeCommit'
  /** Prepare and consume each event before the commit, for a target whose durability is unknown. */ | 'recordAndDeliverBeforeCommit';

/**
 * Bind one Trigger binding to a feed with its selector and target. A target with prepareTrigger, such as
 * newDurableTriggerTarget builds, records events before the feed cursor commits and receives them after; any
 * other target consumes them before the commit. A target with replayTriggerDeliveries is replayed when the
 * runner starts. Throws for invalid wiring.
 */
export function newFeedMember<Change, Payload>(
  definition: TriggerDefinition, binding: TriggerBindingRef, selectEvents: FeedEventSelector<Change, Payload>, target: TriggerTarget<Payload>,
): FeedMember<Change> {
  const validated = validateMemberBinding(definition, binding);
  if (typeof selectEvents !== 'function' || !target) throw new TypeError('poll feed member selector and target are required');
  return new PollMember(validated, selectEvents, target, deliveryModeFor(target), true);
}

/** Return recordBeforeCommit for a target that records events before acknowledgement. */
export function deliveryModeFor<Payload>(target: TriggerTarget<Payload>): MemberDeliveryMode {
  return typeof target.prepareTrigger === 'function' ? 'recordBeforeCommit' : 'deliverBeforeCommit';
}

/** Validate a definition and binding and return the parsed binding; throws on mismatch. */
export function validateMemberBinding(definition: TriggerDefinition, binding: TriggerBindingRef): TriggerBindingRef {
  const parsedDefinition = TriggerDefinitionSchema.parse(definition);
  const parsedBinding = TriggerBindingRefSchema.parse(binding);
  if (parsedBinding.trigger.connectorId !== parsedDefinition.trigger.connectorId || parsedBinding.trigger.triggerName !== parsedDefinition.trigger.triggerName) {
    throw new TypeError('poll trigger binding does not match its definition');
  }
  return parsedBinding;
}

/** Log attributes that identify a Trigger binding, like the Trigger runners'. */
export function bindingLogAttributes(binding: TriggerBindingRef): TriggerLogAttributes {
  return { connector: binding.trigger.connectorId, connection: binding.connection.name, trigger: binding.trigger.triggerName, binding: binding.name };
}

/** A failure to record an event in a durable target before the commit. */
export class RecordFailedError extends Error {
  constructor(cause: unknown) { super(`record Trigger event: ${cause instanceof Error ? cause.message : String(cause)}`, { cause }); this.name = 'RecordFailedError'; }
}

/** A target failure that deliver already logged. */
export class DeliveryFailedError extends Error {
  constructor(cause: unknown) { super(`deliver Trigger event: ${cause instanceof Error ? cause.message : String(cause)}`, { cause }); this.name = 'DeliveryFailedError'; }
}

/** The delivery state of one binding of a poller. */
export class PollMember<Change, Payload> implements FeedMember<Change> {
  #logger: TriggerLogger;
  /** This member's events selected from the page being processed. */
  #staged: TriggerEvent<Payload>[] = [];
  /**
   * The events recorded or consumed at processedRevision; a retry skips their IDs even when the provider now
   * returns a different page.
   */
  #processedRevision = '';
  #processedIds = new Set<string>();
  #processed: TriggerEvent<Payload>[] = [];
  /** Recorded events whose cursor is committed but that the target has not consumed. */
  #queue: TriggerEvent<Payload>[] = [];
  #retryEventId = '';
  #retryFailures = 0;

  constructor(
    readonly binding: TriggerBindingRef,
    private readonly selectEvents: FeedEventSelector<Change, Payload>,
    private readonly target: TriggerTarget<Payload>,
    private readonly mode: MemberDeliveryMode,
    private readonly shouldReplay: boolean,
  ) {
    this.#logger = withTriggerLoggerAttributes(undefined, bindingLogAttributes(binding));
  }

  attachLogger(logger: TriggerLogger | undefined): void { this.#logger = withTriggerLoggerAttributes(logger, bindingLogAttributes(this.binding)); }

  async replay(context: TriggerContext): Promise<void> {
    if (!this.shouldReplay || !this.target.replayTriggerDeliveries) return;
    try { await this.target.replayTriggerDeliveries(context); }
    catch (error: unknown) { throw new Error(`replay Trigger deliveries of binding "${this.binding.name}": ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
  }

  /** Select this member's events from a page fetched at the cursor revision. */
  stage(context: TriggerContext, changes: Change[], previous: (change: Change) => KeyedSnapshotEntry | undefined, revision: string): void {
    this.#staged = [];
    for (const change of changes) {
      for (const event of this.selectEvents(change, previous(change))) {
        if (!event.id?.trim()) {
          logTriggerRecord(this.#logger, context, 'warn', 'trigger event skipped: undeliverable', { event_id: event.id ?? '', error: 'Trigger event ID is required' });
          continue;
        }
        this.#staged.push(event);
      }
    }
    if (this.#processedRevision !== revision) {
      // The cursor moved without this poller's commit, such as after an unknown write outcome.
      this.#settleProcessed();
      this.#processedRevision = revision;
    }
  }

  /**
   * Record or consume the staged events not yet processed at this revision, in order, and stop at the first
   * failure so the cursor does not commit.
   */
  async recordStaged(context: TriggerContext, turn: PollTurn): Promise<void> {
    for (const event of this.#staged) {
      if (this.#processedIds.has(event.id)) continue;
      if (this.mode !== 'deliverBeforeCommit') {
        try { await prepareTriggerDelivery(context, this.target, event); }
        catch (error: unknown) { throw new RecordFailedError(error); }
      }
      if (this.mode !== 'recordBeforeCommit') await this.#deliver(context, turn, event);
      this.#processedIds.add(event.id);
      this.#processed.push(event);
    }
  }

  /** Move recorded events to the delivery queue once the cursor moved past them, by this poller's commit or a replica's. */
  acceptStaged(): void {
    this.#settleProcessed();
    this.#staged = [];
    this.#processedRevision = '';
  }

  /** Queue the events recorded at the processed revision, even ones a refetch dropped. */
  #settleProcessed(): void {
    if (this.mode === 'recordBeforeCommit') this.#queue.push(...this.#processed);
    this.#processed = [];
    this.#processedIds = new Set();
  }

  stagedCount(): number { return this.#staged.length; }

  /** Deliver queued events in order and stop at the first retryable failure. */
  async drain(context: TriggerContext, turn: PollTurn): Promise<void> {
    while (this.#queue.length > 0) {
      await this.#deliver(context, turn, this.#queue[0]!);
      this.#queue.shift();
    }
  }

  /**
   * Make one delivery attempt with the same records and skip accounting as deliverTrigger. A failure is
   * retried on a later poll, before any later event of this member.
   */
  async #deliver(context: TriggerContext, turn: PollTurn, event: TriggerEvent<Payload>): Promise<void> {
    // Ended delivery makes no attempt, as deliverTrigger does; a Dex call cannot see the signal.
    throwIfTriggerAborted(context.signal);
    const failures = this.#retryEventId === event.id ? this.#retryFailures : 0;
    const attempt = triggerAttempt(context);
    try {
      await this.target.handleTrigger(attempt.context, event);
      if (failures > 0 && !attempt.wasSkipped()) logTriggerRecord(this.#logger, context, 'info', 'trigger delivered after retry', { event_id: event.id, attempts: failures + 1 });
    } catch (error: unknown) {
      if (isTriggerUndeliverable(error)) {
        logTriggerRecord(this.#logger, context, 'warn', 'trigger event skipped: undeliverable', { event_id: event.id, ...triggerErrorAttributes(error) });
      } else if (context.signal.aborted) {
        throw new TriggerDeliveryAbortedError();
      } else {
        this.#retryEventId = event.id;
        this.#retryFailures = failures + 1;
        logTriggerRecord(this.#logger, context, 'warn', 'trigger delivery failed; retrying', { event_id: event.id, attempt: failures + 1, delay_ms: turn.failureDelayMs(), ...triggerErrorAttributes(error) });
        throw new DeliveryFailedError(error);
      }
    }
    this.#retryEventId = '';
    this.#retryFailures = 0;
  }
}
