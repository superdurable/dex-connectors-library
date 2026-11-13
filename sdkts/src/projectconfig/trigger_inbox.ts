// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import {
  deliverTrigger, isTriggerUndeliverable, logTriggerRecord, throwIfTriggerAborted, triggerErrorAttributes, withTriggerLoggerAttributes,
  type TriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget,
} from '../trigger_delivery.js';
import { scopePrefix, type Configuration as ProjectConfiguration, type Scope } from './model.js';
import { InvalidRawJSONError, compactGoRawJSON, decodeGoJSONString, encodeGoJSONValue, encodeGoString, formatGoTime, parseTriggerPayloadJSON, rawJSONArrayElements, rawJSONKind, rawJSONObjectMembers } from './raw_json.js';
import { ConflictError, ObjectNotFoundError, OutcomeUnknownError, type ObjectStore } from './storage.js';

/** Schema of one binding's inbox object, shared with the Go Connector SDK. */
export const TriggerInboxSchemaVersion = 'connectors.dex.dev/project-trigger-inbox/v1alpha1';
/** One binding's pending backlog bound; a source stops acknowledging new events beyond it. */
export const MaxPendingTriggerEvents = 1000;
/**
 * One inbox object's size bound in bytes, as in Go. Project storage on S3 holds at most 4 MiB per object
 * in both SDKs, so a hosted inbox is refused by the store beyond that; local and in-memory stores allow 8 MiB.
 */
export const MaxTriggerInboxBytes = 8 << 20;
const maxTriggerInboxAttempts = 8;

/** A binding's pending backlog reached its bound; the source retries after the application consumes events. */
export class TriggerInboxFullError extends Error { constructor() { super('project trigger inbox is full'); this.name = 'TriggerInboxFullError'; } }

/** One application binding of a connector Trigger. */
export interface TriggerInboxKey {
  /** Manifest connector ID. */ connectorId: string;
  /** Application-declared connection the Trigger reads. */ connectionName: string;
  /** Manifest Trigger name. */ triggerName: string;
  /** Application binding of that Trigger. */ bindingName: string;
}

/** One acknowledged Trigger event that the application has not yet consumed. */
export interface PendingTriggerEvent {
  /** Source's stable event identity, decoded as Go decodes JSON strings. */ id: string;
  /**
   * Complete encoded event as raw JSON bytes, kept exactly as stored, like Go's json.RawMessage, so numbers
   * beyond double precision, Go's key order, and bytes that are not valid UTF-8 survive every read and
   * rewrite. Only the consuming application decodes it.
   */
  event: Uint8Array;
}

/**
 * Stores acknowledged Trigger events in project storage until the application consumes them, so a
 * restarted or replaced replica delivers them in order. Writes use compare-and-swap against concurrent
 * replicas. The object key and document match the Go Connector SDK's inbox byte for byte.
 */
export class TriggerInbox {
  readonly #key: string;
  #headExists = false;

  /** Bind one Trigger binding's inbox in scope without reading or writing objects. */
  constructor(private readonly objects: ObjectStore, scope: Scope, key: TriggerInboxKey) {
    if (!objects) throw new TypeError('trigger inbox object store is required');
    const prefix = scopePrefix(scope);
    if (!/^[a-z][a-z0-9-]{1,62}$/.test(key.connectorId) || !/^[a-z][A-Za-z0-9]{1,62}$/.test(key.triggerName)) throw new TypeError('trigger inbox connector or trigger identity is invalid');
    for (const name of [key.connectionName, key.bindingName]) {
      if (!name || name.trim() !== name || name.length > 256 || /[\0\r\n]/.test(name)) throw new TypeError('trigger inbox connection or binding name is invalid');
    }
    const encode = (value: string) => Buffer.from(value).toString('base64url');
    this.#key = `${prefix}/trigger-inboxes/${key.connectorId}/${encode(key.connectionName)}/${key.triggerName}/${encode(key.bindingName)}/head`;
  }

  /** The inbox object key, for diagnostics. */
  get key(): string { return this.#key; }

  /** Return the pending events in acknowledgement order. */
  async pending(): Promise<PendingTriggerEvent[]> { return (await this.#read()).events; }

  /** Append an acknowledged event once; an event whose ID is already pending is unchanged. */
  async add(event: PendingTriggerEvent): Promise<void> {
    if (!event.id?.trim() || !isRawJSON(event.event)) throw new TypeError('trigger inbox event identity or contents are invalid');
    await this.#update(events => {
      if (events.some(pending => pending.id === event.id)) return events;
      if (events.length >= MaxPendingTriggerEvents) throw new TriggerInboxFullError();
      return [...events, event];
    });
  }

  /** Delete a consumed event; removing an absent event succeeds. */
  async remove(eventId: string): Promise<void> {
    await this.#update(events => events.filter(pending => pending.id !== eventId));
  }

  async #update(change: (events: PendingTriggerEvent[]) => PendingTriggerEvent[]): Promise<void> {
    for (let attempt = 0; attempt < maxTriggerInboxAttempts; attempt++) {
      const document = await this.#read();
      const events = change(document.events);
      if (events.length === document.events.length && events.every((event, index) => event.id === document.events[index]!.id)) return;
      const contents = encodeInboxDocument(events);
      if (contents.length > MaxTriggerInboxBytes) throw new TriggerInboxFullError();
      try {
        if (document.etag) await this.objects.compareAndSwapObject(this.#key, document.etag, contents);
        else await this.objects.createObject(this.#key, contents);
        return;
      } catch (error: unknown) {
        if (error instanceof OutcomeUnknownError) {
          try {
            const observed = await this.objects.readObject(this.#key);
            if (Buffer.from(observed.contents).equals(contents)) return;
          } catch { /* An unreadable head is retried below like a conflict. */ }
        }
        if (error instanceof ConflictError || error instanceof OutcomeUnknownError) continue;
        throw error;
      }
    }
    throw new ConflictError();
  }

  async #read(): Promise<{ events: PendingTriggerEvent[]; etag: string }> {
    await this.#createHead();
    let object;
    try { object = await this.objects.readObject(this.#key); }
    catch (error: unknown) {
      if (error instanceof ObjectNotFoundError) return { events: [], etag: '' };
      throw error;
    }
    if (!object.etag) throw new Error('trigger inbox document is invalid');
    return { events: decodeInboxDocument(object.contents), etag: object.etag };
  }

  /**
   * Create the empty head with a create-only write before the first read. S3 refuses a read of a missing
   * key like a permission error unless the reader may list the bucket, so the inbox never relies on
   * reading a missing head; a conflict means the head already exists.
   */
  async #createHead(): Promise<void> {
    if (this.#headExists) return;
    try {
      await this.objects.createObject(this.#key, encodeInboxDocument([]));
      this.#headExists = true;
    } catch (error: unknown) {
      if (error instanceof ConflictError) { this.#headExists = true; return; }
      if (error instanceof OutcomeUnknownError) return;
      throw error;
    }
  }
}

/** Encode the document as Go marshals it: field order, compact output, HTML-safe escapes, raw events compacted verbatim. */
function encodeInboxDocument(events: PendingTriggerEvent[]): Buffer {
  const parts: Buffer[] = [Buffer.from(`{"schemaVersion":${encodeGoString(TriggerInboxSchemaVersion)},"events":[`)];
  events.forEach((event, index) => {
    parts.push(Buffer.from(`${index > 0 ? ',' : ''}{"id":${encodeGoString(event.id)},"event":`), compactGoRawJSON(event.event), Buffer.from('}'));
  });
  parts.push(Buffer.from(']}'));
  return Buffer.concat(parts);
}

/**
 * Report whether a JSON object key names a Go struct field, as encoding/json matches keys: exactly or
 * case-insensitively with Unicode simple folding, under which U+017F folds to s and U+212A to k.
 */
function isGoFieldKey(key: string, field: string): boolean {
  return key === field || key.toLowerCase().replaceAll('ſ', 's') === field.toLowerCase();
}

/**
 * Decode one JSON value into a Go string field: a string decodes as Go decodes strings, and null leaves the
 * field unchanged, as encoding/json does; any other kind is a type error.
 */
function decodeGoStringField(raw: Uint8Array, current: string): string {
  const kind = rawJSONKind(raw);
  if (kind === 'null') return current;
  if (kind !== 'string') throw new InvalidRawJSONError();
  return decodeGoJSONString(Buffer.from(raw).subarray(firstByteIndex(raw)));
}

function firstByteIndex(raw: Uint8Array): number {
  let index = 0;
  while (raw[index] === 0x20 || raw[index] === 0x09 || raw[index] === 0x0a || raw[index] === 0x0d) index++;
  return index;
}

/**
 * Decode the document strictly, as Go's DisallowUnknownFields decoder does: every occurrence of a field,
 * duplicates included, must have the field's type, and the last one wins. Each event keeps its raw bytes;
 * a null or missing event is JSON null, as Go re-marshals a nil json.RawMessage.
 */
function decodeInboxDocument(contents: Uint8Array): PendingTriggerEvent[] {
  try {
    if (rawJSONKind(contents) !== 'object') throw new InvalidRawJSONError();
    let schemaVersion = '';
    let events: PendingTriggerEvent[] = [];
    for (const member of rawJSONObjectMembers(contents)) {
      if (isGoFieldKey(member.key, 'schemaVersion')) { schemaVersion = decodeGoStringField(member.value, schemaVersion); continue; }
      if (!isGoFieldKey(member.key, 'events')) throw new InvalidRawJSONError();
      const kind = rawJSONKind(member.value);
      if (kind === 'null') { events = []; continue; }
      if (kind !== 'array') throw new InvalidRawJSONError();
      events = rawJSONArrayElements(member.value).map(decodePendingEvent);
    }
    if (schemaVersion !== TriggerInboxSchemaVersion) throw new InvalidRawJSONError();
    return events;
  } catch { throw new Error('trigger inbox document is invalid'); }
}

function decodePendingEvent(raw: Buffer): PendingTriggerEvent {
  const kind = rawJSONKind(raw);
  if (kind === 'null') return { id: '', event: Buffer.from('null') };
  if (kind !== 'object') throw new InvalidRawJSONError();
  let id = '';
  let event: Uint8Array = Buffer.from('null');
  for (const member of rawJSONObjectMembers(raw)) {
    if (isGoFieldKey(member.key, 'id')) id = decodeGoStringField(member.value, id);
    else if (isGoFieldKey(member.key, 'event')) event = member.value;
    else throw new InvalidRawJSONError();
  }
  return { id, event };
}

function isRawJSON(value: Uint8Array): boolean {
  try { compactGoRawJSON(value); return true; } catch { return false; }
}

/**
 * Encode a Trigger event in the Go Connector SDK's form: eventId, RFC 3339 occurredAt, and the payload
 * encoded by encodeGoJSONValue, which sorts object keys like a Go map and refuses values Go cannot encode.
 */
export function encodeTriggerEvent<Payload>(event: TriggerEvent<Payload>): Buffer {
  return Buffer.from(`{"eventId":${encodeGoString(event.id)},"occurredAt":${encodeGoString(formatGoTime(event.occurredAt))},"payload":${encodeGoJSONValue(event.payload)}}`);
}

/**
 * Decode an event that encodeTriggerEvent or the Go Connector SDK wrote, as Go's json.Unmarshal does into
 * its TriggerEvent: unknown fields are ignored, keys match case-insensitively, and the last occurrence of a
 * field wins. decodePayload receives the payload's raw JSON as UTF-8 text and as the exact stored bytes, so it can
 * keep values a double cannot hold.
 */
export function decodeTriggerEvent<Payload>(encoded: Uint8Array, decodePayload: TriggerPayloadDecoder<Payload>): TriggerEvent<Payload> {
  if (rawJSONKind(encoded) !== 'object') throw new TypeError('pending Trigger event is not a JSON object');
  let id = '';
  let occurredAt = '';
  let payload: Uint8Array = Buffer.from('null');
  for (const member of rawJSONObjectMembers(encoded)) {
    if (isGoFieldKey(member.key, 'eventId')) id = decodeGoStringField(member.value, id);
    else if (isGoFieldKey(member.key, 'occurredAt')) occurredAt = decodeGoStringField(member.value, occurredAt);
    else if (isGoFieldKey(member.key, 'payload')) payload = member.value;
  }
  const time = new Date(occurredAt);
  if (Number.isNaN(time.getTime())) throw new TypeError('pending Trigger event time is invalid');
  // The decoder gets its own copy, because stores share the bytes they cache.
  return { id, occurredAt: time, payload: decodePayload(Buffer.from(payload).toString('utf8'), new Uint8Array(payload)) };
}

/**
 * Strictly decode the configuration of one Trigger binding with schema. Throws ObjectNotFoundError when
 * the project has no such binding, and the schema's error when the configuration violates it.
 */
export function decodeTriggerConfiguration<BindingConfiguration>(configuration: ProjectConfiguration, binding: TriggerInboxKey, schema: z.ZodType<BindingConfiguration>): BindingConfiguration {
  const found = configuration.triggerBindings.find(entry => entry.connectorId === binding.connectorId && entry.connectionName === binding.connectionName && entry.triggerName === binding.triggerName && entry.bindingName === binding.bindingName);
  if (!found) throw new ObjectNotFoundError();
  return schema.parse(found.configuration);
}

/**
 * Decodes a replayed Trigger payload. payloadJSON is the raw JSON decoded as UTF-8 text, with each invalid
 * sequence replaced as TextDecoder replaces it; payloadBytes are a copy of the exact stored bytes, which the
 * decoder may change without affecting the inbox, and which Go-compatible
 * decoding such as parseTriggerPayloadJSON reads.
 */
export type TriggerPayloadDecoder<Payload> = (payloadJSON: string, payloadBytes: Uint8Array) => Payload;

/** Optional behavior of newDurableTriggerTarget. */
export interface DurableTriggerOptions<Payload> {
  /** Receives the inbox's records, with the binding identity; omitted uses defaultTriggerLogger. */ logger?: TriggerLogger;
  /**
   * Decode a replayed payload from its raw JSON text. Omitted uses parseTriggerPayloadJSON, which has the
   * precision of Go's encoding/json decoding into any; a decoder that needs exact integers beyond 2^53 reads
   * the raw text itself.
   */
  decodePayload?: TriggerPayloadDecoder<Payload>;
}

/**
 * Store acknowledged events in the project Trigger inbox until the target consumes them.
 *
 * The target consumes an event by resolving or by rejecting with an UndeliverableTriggerError; any other
 * error keeps the event pending. Replay delivers pending events in order with deliverTrigger. Inbox read
 * and write failures reject like target failures, so callers retry them. When only the removal of a
 * consumed event fails, a retry removes it without invoking the target again.
 *
 * The inbox logs WARN "trigger event skipped: undeliverable" for every event it consumes as undeliverable,
 * ERROR for every inbox read, write, or removal failure, and INFO at the start and end of a replay that
 * finds pending events.
 */
export function newDurableTriggerTarget<Payload>(inbox: TriggerInbox, key: TriggerInboxKey, target: TriggerTarget<Payload>, options: DurableTriggerOptions<Payload> = {}): TriggerTarget<Payload> {
  if (!inbox || !target) throw new TypeError('project trigger inbox and Trigger target are required');
  return new DurableTriggerTarget(inbox, target, withTriggerLoggerAttributes(options.logger, {
    connector: key.connectorId, connection: key.connectionName, trigger: key.triggerName, binding: key.bindingName,
  }), options.decodePayload ?? ((_payloadJSON, payloadBytes) => parseTriggerPayloadJSON(payloadBytes) as Payload));
}

class DurableTriggerTarget<Payload> implements TriggerTarget<Payload> {
  /** Serializes inbox access, as the Go target's mutex does. */
  #lockTail: Promise<void> = Promise.resolve();
  /** Consumed events whose removal failed, and whether each was consumed as undeliverable. */
  readonly #pendingRemoval = new Map<string, boolean>();

  constructor(private readonly inbox: TriggerInbox, private readonly target: TriggerTarget<Payload>, private readonly logger: TriggerLogger, private readonly decodePayload: TriggerPayloadDecoder<Payload>) {}

  async prepareTrigger(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void> {
    if (!event.id?.trim()) throw new TypeError('Trigger event ID is required');
    const encoded = encodeTriggerEvent(event);
    await this.#withLock(async () => {
      // A source that abandoned this acknowledgement, such as a timed-out webhook request, must not record it late.
      throwIfTriggerAborted(context.signal);
      try { await this.inbox.add({ id: event.id, event: encoded }); }
      catch (error: unknown) {
        logTriggerRecord(this.logger, context, 'error', 'trigger inbox write failed', { event_id: event.id, ...triggerErrorAttributes(error) });
        throw error;
      }
    });
  }

  async handleTrigger(context: TriggerContext, event: TriggerEvent<Payload>): Promise<void> {
    await this.#withLock(async () => { await this.#consume(context, event); });
  }

  /**
   * Deliver the events pending at the call in order. The lock is held only for one attempt, so
   * prepareTrigger does not wait for a pending event's backoff. A source must deliver new events only
   * after replay resolves, or a new event could overtake an older pending one.
   */
  async replayTriggerDeliveries(context: TriggerContext): Promise<void> {
    let pending: PendingTriggerEvent[];
    try { pending = await this.inbox.pending(); }
    catch (error: unknown) {
      logTriggerRecord(this.logger, context, 'error', 'trigger inbox read failed', triggerErrorAttributes(error));
      throw error;
    }
    if (pending.length === 0) return;
    logTriggerRecord(this.logger, context, 'info', 'replaying pending trigger events', { count: pending.length });
    let delivered = 0, skipped = 0;
    for (const [index, stored] of pending.entries()) {
      let event: TriggerEvent<Payload>;
      try { event = decodeTriggerEvent(stored.event, this.decodePayload); }
      catch (error: unknown) {
        logTriggerRecord(this.logger, context, 'error', 'trigger inbox event decode failed', { event_id: stored.id, ...triggerErrorAttributes(error) });
        throw new Error('decode pending Trigger event', { cause: error });
      }
      let isEventSkipped = false;
      try {
        await deliverTrigger(context, { handleTrigger: async (attemptContext, attemptEvent) => { isEventSkipped = await this.#withLock(() => this.#handlePending(attemptContext, attemptEvent)); } }, event, { logger: this.logger });
      } catch (error: unknown) {
        logTriggerRecord(this.logger, context, 'info', 'finished replaying pending trigger events', { delivered, skipped, remaining: pending.length - index, ...triggerErrorAttributes(error) });
        throw error;
      }
      if (isEventSkipped) skipped++; else delivered++;
    }
    logTriggerRecord(this.logger, context, 'info', 'finished replaying pending trigger events', { delivered, skipped, remaining: 0 });
  }

  /** Make one replay attempt, skipping an event another delivery already consumed; report an undeliverable skip. */
  async #handlePending(context: TriggerContext, event: TriggerEvent<Payload>): Promise<boolean> {
    let pending: PendingTriggerEvent[];
    try { pending = await this.inbox.pending(); }
    catch (error: unknown) {
      logTriggerRecord(this.logger, context, 'error', 'trigger inbox read failed', { event_id: event.id, ...triggerErrorAttributes(error) });
      throw error;
    }
    return pending.some(stored => stored.id === event.id) ? this.#consume(context, event) : false;
  }

  /** Deliver one event and remove it once consumed; report whether it was consumed as undeliverable. The caller holds the lock. */
  async #consume(context: TriggerContext, event: TriggerEvent<Payload>): Promise<boolean> {
    let isSkipped = this.#pendingRemoval.get(event.id);
    if (isSkipped === undefined) {
      isSkipped = false;
      try { await this.target.handleTrigger(context, event); }
      catch (error: unknown) {
        if (!isTriggerUndeliverable(error)) throw error;
        isSkipped = true;
        logTriggerRecord(this.logger, context, 'warn', 'trigger event skipped: undeliverable', { event_id: event.id, ...triggerErrorAttributes(error) });
      }
    }
    // The enclosing delivery attempt must not also report this event as delivered.
    if (isSkipped) context.recordSkip();
    try { await this.inbox.remove(event.id); }
    catch (error: unknown) {
      this.#pendingRemoval.set(event.id, isSkipped);
      logTriggerRecord(this.logger, context, 'error', 'trigger inbox remove failed', { event_id: event.id, ...triggerErrorAttributes(error) });
      throw error;
    }
    this.#pendingRemoval.delete(event.id);
    return isSkipped;
  }

  #withLock<Result>(work: () => Promise<Result>): Promise<Result> {
    const result = this.#lockTail.then(work);
    this.#lockTail = result.then(() => undefined, () => undefined);
    return result;
  }
}
