// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Prefix of every UndeliverableTriggerError message; RPC targets recognize a handler's marker by it. */
export const UndeliverableTriggerMessage = 'Trigger event is undeliverable';
/**
 * The error name of every UndeliverableTriggerError. A Worker reports it as the error type, and only this
 * SDK-owned value, never a generic name such as "UndeliverableTriggerError", marks a handler's rejection.
 */
export const UndeliverableTriggerErrorType = 'ConnectorsSDK.UndeliverableTriggerError';

/**
 * Reports that no retry can deliver a Trigger event to its target.
 *
 * Durable Trigger inboxes, Trigger runners, and deliverTrigger consume the event instead of retrying it.
 * A Dex RPC handler throws it to reject a Trigger event; the Worker reports its name,
 * UndeliverableTriggerErrorType, as the error type and its message as the detail, which the RPC Trigger
 * target recognizes. Throw the marker itself, not an error that wraps it.
 */
export class UndeliverableTriggerError extends Error {
  /** Wrap the reason; the message is the marker prefix followed by the reason's message. */
  constructor(cause?: unknown) {
    const reason = cause instanceof Error ? cause.message : cause === undefined ? '' : String(cause);
    super(reason ? `${UndeliverableTriggerMessage}: ${reason}` : UndeliverableTriggerMessage, cause === undefined ? undefined : { cause });
    this.name = UndeliverableTriggerErrorType;
  }
}

/**
 * Mark an error so Trigger delivery consumes the event instead of retrying it.
 * Returns undefined for undefined, the error unchanged when it is already an UndeliverableTriggerError,
 * and otherwise a new marker whose cause is the error.
 */
export function markTriggerUndeliverable(error: unknown): UndeliverableTriggerError;
export function markTriggerUndeliverable(error: undefined): undefined;
export function markTriggerUndeliverable(error: unknown): UndeliverableTriggerError | undefined {
  if (error === undefined) return undefined;
  return error instanceof UndeliverableTriggerError ? error : new UndeliverableTriggerError(error);
}

/** Report whether the error, or an error in its cause chain, is an UndeliverableTriggerError. */
export function isTriggerUndeliverable(error: unknown): boolean {
  for (let current = error, depth = 0; current instanceof Error && depth < 32; current = current.cause, depth++) {
    if (current instanceof UndeliverableTriggerError) return true;
  }
  return false;
}

/** Severity of one Trigger delivery record. */
export type TriggerLogLevel = 'debug' | 'info' | 'warn' | 'error';

/** Safe structured values in a Trigger record: IDs, counts, enum values, and error messages, never payloads. */
export type TriggerLogAttributes = Readonly<Record<string, string | number | boolean>>;

/** Receives Trigger delivery records. Implementations must not throw. */
export interface TriggerLogger {
  /** Report whether records at level are written, so callers can skip building them. */
  isEnabled(level: TriggerLogLevel): boolean;
  /** Write one record. */
  log(level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes): void;
}

const triggerLogLevelOrder: Record<TriggerLogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

/** Create a logger that writes one JSON line per record at or above minimumLevel to stderr. */
export function newConsoleTriggerLogger(minimumLevel: TriggerLogLevel = 'info'): TriggerLogger {
  return {
    isEnabled: level => triggerLogLevelOrder[level] >= triggerLogLevelOrder[minimumLevel],
    log(level, message, attributes) {
      if (triggerLogLevelOrder[level] < triggerLogLevelOrder[minimumLevel]) return;
      process.stderr.write(`${JSON.stringify({ time: new Date().toISOString(), level: level.toUpperCase(), msg: message, ...attributes })}\n`);
    },
  };
}

/** The logger used when an option omits one: INFO and above as JSON lines on stderr. */
export const defaultTriggerLogger: TriggerLogger = newConsoleTriggerLogger('info');

/** Return a logger that adds attributes to every record; a record keeps its own value for a shared key. */
export function withTriggerLoggerAttributes(logger: TriggerLogger | undefined, attributes: TriggerLogAttributes): TriggerLogger {
  const base = logger ?? defaultTriggerLogger;
  return { isEnabled: level => base.isEnabled(level), log: (level, message, recordAttributes) => base.log(level, message, { ...attributes, ...recordAttributes }) };
}

/**
 * One Trigger delivery's cancellation, log attributes, and skip recorder, passed to every target call.
 * Create the root with newTriggerContext and derive attempts with triggerAttempt.
 */
export interface TriggerContext {
  /** Ends delivery: sources stop, retries stop waiting, and Dex calls are abandoned. */ readonly signal: AbortSignal;
  /** Event-scoped IDs, such as a channel or thread ID, added to every delivery record. */ readonly logAttributes: TriggerLogAttributes;
  /** Record that a component inside this attempt consumed the event as undeliverable and logged the skip. */ recordSkip(): void;
}

/** Create a root Trigger context; a missing signal never aborts. */
export function newTriggerContext(signal?: AbortSignal): TriggerContext {
  return { signal: signal ?? new AbortController().signal, logAttributes: {}, recordSkip: () => undefined };
}

/**
 * Return a context whose delivery records also carry attributes: those of deliverTrigger, the durable
 * inbox, the Dex targets, and Trigger runners inside it. Never pass message text, payloads, or credentials.
 */
export function withTriggerLogAttributes(context: TriggerContext, attributes: TriggerLogAttributes): TriggerContext {
  return { signal: context.signal, logAttributes: { ...context.logAttributes, ...attributes }, recordSkip: () => context.recordSkip() };
}

/**
 * Start one delivery attempt for a source that retries on its own schedule instead of with deliverTrigger.
 * Pass the returned context to handleTrigger. When it resolves, wasSkipped reports whether a component
 * inside the attempt consumed the event as undeliverable and already logged that skip.
 */
export function triggerAttempt(context: TriggerContext): { context: TriggerContext; wasSkipped(): boolean } {
  let isSkipped = false;
  return {
    context: { signal: context.signal, logAttributes: context.logAttributes, recordSkip: () => { isSkipped = true; context.recordSkip(); } },
    wasSkipped: () => isSkipped,
  };
}

/** Log one record through logger with the context's event-scoped attributes; record attributes win. */
export function logTriggerRecord(logger: TriggerLogger | undefined, context: TriggerContext, level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes = {}): void {
  const target = logger ?? defaultTriggerLogger;
  if (!target.isEnabled(level)) return;
  try { target.log(level, message, { ...context.logAttributes, ...attributes }); }
  catch { /* Logging is best-effort because a logger failure cannot change Trigger delivery state. */ }
}

/**
 * Error attributes for a record: the error's class, its first cause's class, and a Dex service error's
 * gRPC code. Messages are never logged, because a handler, provider, or credential store may put secrets
 * or payload text in them.
 */
export function triggerErrorAttributes(error: unknown): TriggerLogAttributes {
  const attributes: Record<string, string | number> = { error_type: triggerErrorType(error) };
  if (error instanceof Error && error.cause !== undefined) attributes.error_cause_type = triggerErrorType(error.cause);
  const code = error instanceof Error ? (error as { code?: unknown }).code : undefined;
  if (typeof code === 'number' && Number.isSafeInteger(code)) attributes.error_code = code;
  return attributes;
}

/** The class name of an error, or its JavaScript type for a thrown non-error. */
function triggerErrorType(error: unknown): string {
  if (!(error instanceof Error)) return typeof error;
  const name = error.constructor?.name ?? '';
  return /^[A-Za-z_$][A-Za-z0-9_$]{0,63}$/.test(name) ? name : 'Error';
}

const triggerRetryInitialDelayMs = 250;
const triggerRetryMaxDelayMs = 30_000;

/** Return the wait in milliseconds after failed attempt number attempt, starting at 1. */
export function triggerRetryDelayMs(attempt: number): number {
  let delay = triggerRetryInitialDelayMs;
  for (let completed = 1; completed < attempt && delay < triggerRetryMaxDelayMs; completed++) delay *= 2;
  return Math.min(delay, triggerRetryMaxDelayMs);
}

/** Optional behavior of deliverTrigger and the Dex Trigger targets. */
export interface TriggerOptions {
  /** Receives delivery records; omitted uses defaultTriggerLogger. Records carry IDs, never payloads. */ logger?: TriggerLogger;
}

/** One provider event with a stable provider identity. */
export interface TriggerEvent<Payload> {
  /** Stable provider identifier; also the Flow-start request ID. */ id: string;
  /** When the provider event occurred. */ occurredAt: Date;
  /** Connector-normalized provider event body. */ payload: Payload;
}

/**
 * Handles one event after the provider transport acknowledges receipt.
 * handleTrigger resolves when it consumes the event, rejects with an UndeliverableTriggerError when no
 * retry can deliver it, or rejects with another error to keep the event pending for a retry.
 * prepareTrigger, when present, persists the event before the provider acknowledges it.
 * replayTriggerDeliveries, when present, redelivers events persisted before a previous process stopped;
 * it retries target failures, resolves once every event pending at the call was consumed, and rejects
 * when the context ends first or the persisted events cannot be read.
 */
export interface TriggerTarget<Payload> {
  /** Consume, retry, or mark one provider event undeliverable. */ handleTrigger(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void>;
  /** Persist an event before provider acknowledgement. */ prepareTrigger?(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void>;
  /** Drain events left pending by an earlier process. */ replayTriggerDeliveries?(context: TriggerContext): Promise<void>;
}

/** Ask a durable target to persist an event before provider acknowledgement; other targets need nothing. */
export async function prepareTriggerDelivery<Payload>(context: TriggerContext, target: TriggerTarget<Payload>, event: TriggerEvent<Payload>): Promise<void> {
  if (target.prepareTrigger) await target.prepareTrigger(context, event);
}

/** Report whether a target records and replays events, such as newDurableTriggerTarget builds. */
export function hasDurableTriggerInbox<Payload>(target: TriggerTarget<Payload>): boolean {
  return typeof target.prepareTrigger === 'function' && typeof target.replayTriggerDeliveries === 'function';
}

/** Error thrown when a Trigger context's signal ends delivery. */
export class TriggerDeliveryAbortedError extends Error {
  constructor() { super('Trigger delivery was aborted'); this.name = 'TriggerDeliveryAbortedError'; }
}

/** Throw TriggerDeliveryAbortedError when the signal has ended. */
export function throwIfTriggerAborted(signal: AbortSignal): void {
  if (signal.aborted) throw new TriggerDeliveryAbortedError();
}

/** Wait for delayMs or reject with TriggerDeliveryAbortedError as soon as the signal ends. */
export function waitForTriggerDelay(signal: AbortSignal, delayMs: number): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(new TriggerDeliveryAbortedError()); return; }
    const onAbort = () => { clearTimeout(timer); reject(new TriggerDeliveryAbortedError()); };
    const timer = setTimeout(() => { signal.removeEventListener('abort', onAbort); resolve(); }, delayMs);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

/**
 * Hand one acknowledged event to target until target consumes it.
 *
 * A resolved attempt or an UndeliverableTriggerError consumes the event. Every other error is retried
 * after a capped exponential delay that starts at 250 milliseconds and doubles up to 30 seconds. It
 * resolves once the event is consumed and rejects with TriggerDeliveryAbortedError as soon as the
 * context's signal ends, including while it waits between attempts.
 *
 * It logs WARN "trigger delivery failed; retrying" with the attempt and next delay before every wait,
 * INFO "trigger delivered after retry" when a later attempt succeeds, and WARN "trigger event skipped:
 * undeliverable" when the target rejects with an UndeliverableTriggerError. A target that consumes an
 * undeliverable event itself, such as a durable inbox, logs that skip instead.
 */
export async function deliverTrigger<Payload>(context: TriggerContext, target: TriggerTarget<Payload>, event: TriggerEvent<Payload>, options: TriggerOptions = {}): Promise<void> {
  for (let attempt = 1; ; attempt++) {
    throwIfTriggerAborted(context.signal);
    const started = triggerAttempt(context);
    try {
      await target.handleTrigger(started.context, event);
      if (attempt > 1 && !started.wasSkipped()) logTriggerRecord(options.logger, context, 'info', 'trigger delivered after retry', { event_id: event.id, attempts: attempt });
      return;
    } catch (error: unknown) {
      if (isTriggerUndeliverable(error)) {
        logTriggerRecord(options.logger, context, 'warn', 'trigger event skipped: undeliverable', { event_id: event.id, ...triggerErrorAttributes(error) });
        return;
      }
      // The attempt failed because delivery ended, so there is no retry to report.
      throwIfTriggerAborted(context.signal);
      const delayMs = triggerRetryDelayMs(attempt);
      logTriggerRecord(options.logger, context, 'warn', 'trigger delivery failed; retrying', { event_id: event.id, attempt, delay_ms: delayMs, ...triggerErrorAttributes(error) });
      await waitForTriggerDelay(context.signal, delayMs);
    }
  }
}
