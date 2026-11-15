// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { scopePrefix, type Scope } from './model.js';
import { InvalidRawJSONError, compactGoRawJSON, decodeGoStringField, encodeGoString, isGoFieldKey, rawJSONArrayElements, rawJSONKind, rawJSONObjectMembers } from './raw_json.js';
import { ConflictError, ObjectNotFoundError, OutcomeUnknownError, type ObjectStore, type ProjectObject } from './storage.js';
import { MaxTriggerInboxBytes, type TriggerInboxKey } from './trigger_inbox.js';

/** Schema of one poll cursor document, shared with the Go Connector SDK. */
export const PollCursorSchemaVersion = 'connectors.dex.dev/project-poll-cursor/v1alpha1';
/** Bound of PollCursorState.token in UTF-8 bytes. */
export const MaxPollCursorTokenBytes = 64 << 10;
/** Hard bound on the entries of one stored keyed snapshot. */
export const MaxKeyedSnapshotEntries = 100_000;
/** Bound of one encoded poll cursor document in bytes, as in Go; S3 stores at most 4 MiB per object. */
export const MaxPollCursorDocumentBytes = MaxTriggerInboxBytes;
const maxKeyedSnapshotKeyBytes = 1 << 10;

/**
 * Reports a poll cursor state over a storage bound: a token over MaxPollCursorTokenBytes, a snapshot over its
 * entry bound, or an encoded document over 8 MiB. No retry of the same state can succeed; the connector or
 * its configuration must produce a smaller state.
 */
export class PollCursorTooLargeError extends Error {
  /** Describe which bound the state exceeds. */
  constructor(detail: string) { super(`project poll cursor state exceeds its bound: ${detail}`); this.name = 'PollCursorTooLargeError'; }
}

/**
 * Reports a stored poll cursor document that fails strict decoding or has another schema version. The store
 * never overwrites such a document; an operator must inspect it.
 */
export class PollCursorDocumentInvalidError extends Error {
  /** Wrap the decoding or validation failure, when one is known. */
  constructor(cause?: unknown) {
    super('project poll cursor document is invalid', cause === undefined ? undefined : { cause });
    this.name = 'PollCursorDocumentInvalidError';
  }
}

/** One Trigger binding's poll cursor identity: the same four parts as its Trigger inbox key. */
export type PollCursorKey = TriggerInboxKey;

/** One poll cursor shared by several Trigger bindings of one connection, such as one provider change feed. */
export interface PollFeedKey {
  /** Manifest connector ID. */ connectorId: string;
  /** Application-declared connection the feed reads. */ connectionName: string;
  /** The connector's stable lower camel case feed name, such as "driveChanges". */ feedName: string;
}

/**
 * A committed provider position. Every field is part of the stored document contract
 * "connectors.dex.dev/project-poll-cursor/v1alpha1"; numbers are integers, as Go's int64 fields are.
 */
export interface PollCursorState {
  /** The connector's opaque encoded position, at most MaxPollCursorTokenBytes UTF-8 bytes. Empty is valid. */ token: string;
  /** Optional bounded keyed snapshot committed atomically with token; undefined when unused. */ snapshot?: KeyedSnapshot | undefined;
  /** Time of the latest seed, in milliseconds since the Unix epoch. */ seededAtUnixMs: number;
  /** Time of this commit, in milliseconds since the Unix epoch. */ committedAtUnixMs: number;
  /**
   * Counts every commit, including seeds. It is diagnostic and makes each commit's bytes unique, so an
   * unknown write outcome is reconciled by comparing bytes.
   */
  commitCount: number;
  /** Counts reseeds after the provider rejected the cursor permanently. */ resetCount: number;
  /** Time of the latest reseed in milliseconds, or undefined before the first. */ lastResetAtUnixMs?: number | undefined;
}

/** A bounded set of tracked provider items stored with a poll cursor, sorted by the UTF-8 bytes of unique keys. */
export interface KeyedSnapshot {
  /** The tracked items, sorted by key. */ entries: KeyedSnapshotEntry[];
}

/** One tracked provider item. */
export interface KeyedSnapshotEntry {
  /** The connector's stable item key, 1 byte to 1 KiB of UTF-8. */ key: string;
  /** The item's content hash: lowercase hex, 16 to 64 characters. */ hash: string;
  /**
   * The commitCount of the commit that last added, changed, or reported this entry. Zero in a state being
   * committed means "this commit"; the poll Trigger kit replaces it before writing.
   */
  seen: number;
  /** The item's raw JSON bytes, kept only when a poller stores values; undefined omits it, never null. */ value?: Uint8Array | undefined;
}

/** One read of a poll cursor head. */
export interface PollCursorRecord {
  /** The committed position, or undefined until the cursor is seeded. */ state: PollCursorState | undefined;
  /** Opaque compare-and-swap token for replacePollCursor; empty before the head exists. */ revision: string;
}

/**
 * Reads and conditionally replaces one poll cursor head in project storage, beside the Trigger inboxes.
 * Concurrent replicas are ordered by compare-and-swap. The object key and document match the Go Connector
 * SDK byte for byte.
 */
export interface PollCursorStore {
  /** The cursor object key, for diagnostics. */ readonly key: string;
  /**
   * Return the current record. Before its first read the store creates the unseeded head with a create-only
   * write, so it never reads a missing key and a restart learns whether the cursor was seeded without a
   * provider call. Rejects with PollCursorDocumentInvalidError when the stored document fails strict decoding.
   */
  readPollCursor(): Promise<PollCursorRecord>;
  /**
   * Write state only while the head is at expectedRevision, the revision of the read the caller decided on.
   * Rejects with ConflictError when another writer changed the head first and with PollCursorTooLargeError
   * for a state over a storage bound. An unknown write outcome is reconciled by reading the head back and
   * comparing bytes; a mismatch is ConflictError. It never retries with another precondition.
   */
  replacePollCursor(expectedRevision: string, state: PollCursorState): Promise<PollCursorRecord>;
}

/**
 * Bind one Trigger binding's poll cursor in scope without reading or writing objects. The key is
 * "<scope prefix>/poll-cursors/<connectorId>/<b64url(connection)>/<trigger>/<b64url(binding)>/head".
 * Throws TypeError for an invalid identity.
 */
export function newPollCursorStore(objects: ObjectStore, scope: Scope, key: PollCursorKey): PollCursorStore {
  const prefix = pollCursorPrefix(objects, scope, key.connectorId, key.triggerName, [key.connectionName, key.bindingName]);
  const encode = (value: string) => Buffer.from(value).toString('base64url');
  return new ProjectPollCursorStore(objects, `${prefix}/poll-cursors/${key.connectorId}/${encode(key.connectionName)}/${key.triggerName}/${encode(key.bindingName)}/head`);
}

/**
 * Bind one shared feed's poll cursor in scope without reading or writing objects. The key is
 * "<scope prefix>/poll-feeds/<connectorId>/<b64url(connection)>/<feed>/head". Throws TypeError for an
 * invalid identity.
 */
export function newPollFeedCursorStore(objects: ObjectStore, scope: Scope, key: PollFeedKey): PollCursorStore {
  const prefix = pollCursorPrefix(objects, scope, key.connectorId, key.feedName, [key.connectionName]);
  return new ProjectPollCursorStore(objects, `${prefix}/poll-feeds/${key.connectorId}/${Buffer.from(key.connectionName).toString('base64url')}/${key.feedName}/head`);
}

/** Go's unicode.IsSpace set at the start or end of a name. */
const goSpaceAtEdge = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]$/;

function pollCursorPrefix(objects: ObjectStore, scope: Scope, connectorId: string, triggerOrFeedName: string, names: string[]): string {
  if (!objects) throw new TypeError('poll cursor object store is required');
  const prefix = scopePrefix(scope);
  if (!/^[a-z][a-z0-9-]{1,62}$/.test(connectorId) || !/^[a-z][A-Za-z0-9]{1,62}$/.test(triggerOrFeedName)) throw new TypeError('poll cursor connector, trigger, or feed identity is invalid');
  for (const name of names) {
    if (typeof name !== 'string' || !name || goSpaceAtEdge.test(name) || Buffer.byteLength(name) > 256 || /[\0\r\n]/.test(name)) throw new TypeError('poll cursor connection or binding name is invalid');
  }
  return prefix;
}

class ProjectPollCursorStore implements PollCursorStore {
  /** Records that the head was created or found, so no later read asks for a missing key. */
  #headExists = false;

  constructor(private readonly objects: ObjectStore, readonly key: string) {}

  async readPollCursor(): Promise<PollCursorRecord> {
    await this.#createHead();
    let object: ProjectObject;
    try { object = await this.objects.readObject(this.key); }
    catch (error: unknown) {
      if (error instanceof ObjectNotFoundError) return { state: undefined, revision: '' };
      throw error;
    }
    const state = decodePollCursorDocument(object.contents);
    if (!object.etag) throw new PollCursorDocumentInvalidError(new Error('missing revision'));
    this.#headExists = true;
    return { state, revision: object.etag };
  }

  async replacePollCursor(expectedRevision: string, state: PollCursorState): Promise<PollCursorRecord> {
    const contents = encodePollCursorDocument(state);
    let written: ProjectObject;
    try {
      written = expectedRevision === '' ? await this.objects.createObject(this.key, contents) : await this.objects.compareAndSwapObject(this.key, expectedRevision, contents);
    } catch (error: unknown) {
      if (!(error instanceof OutcomeUnknownError)) throw error;
      let observed: ProjectObject;
      try { observed = await this.objects.readObject(this.key); }
      catch { throw error; }
      if (!Buffer.from(observed.contents).equals(contents)) throw new ConflictError();
      written = observed;
    }
    this.#headExists = true;
    return { state, revision: written.etag };
  }

  /** Create the unseeded head before the first read; a conflict means it already exists. */
  async #createHead(): Promise<void> {
    if (this.#headExists) return;
    try {
      await this.objects.createObject(this.key, encodePollCursorDocument(undefined));
      this.#headExists = true;
    } catch (error: unknown) {
      if (error instanceof ConflictError) { this.#headExists = true; return; }
      if (error instanceof OutcomeUnknownError) return;
      throw error;
    }
  }
}

/** Encode the document as Go marshals it: field order, compact output, HTML-safe escapes, values compacted. */
export function encodePollCursorDocument(state: PollCursorState | undefined): Buffer {
  const parts: Buffer[] = [Buffer.from(`{"schemaVersion":${encodeGoString(PollCursorSchemaVersion)},"state":`)];
  if (state === undefined) parts.push(Buffer.from('null'));
  else {
    validatePollCursorState(state);
    parts.push(Buffer.from(`{"token":${encodeGoString(state.token)},"snapshot":`));
    if (state.snapshot === undefined) parts.push(Buffer.from('null'));
    else {
      parts.push(Buffer.from('{"entries":['));
      state.snapshot.entries.forEach((entry, index) => {
        parts.push(Buffer.from(`${index > 0 ? ',' : ''}{"key":${encodeGoString(entry.key)},"hash":${encodeGoString(entry.hash)},"seen":${entry.seen}`));
        // Stored values keep their bytes, which Go never re-validates as UTF-8.
        if (entry.value !== undefined) parts.push(Buffer.from(',"value":'), compactGoRawJSON(entry.value));
        parts.push(Buffer.from('}'));
      });
      parts.push(Buffer.from(']}'));
    }
    parts.push(Buffer.from(`,"seededAtUnixMs":${state.seededAtUnixMs},"committedAtUnixMs":${state.committedAtUnixMs},"commitCount":${state.commitCount},"resetCount":${state.resetCount},"lastResetAtUnixMs":${state.lastResetAtUnixMs ?? 'null'}}`));
  }
  parts.push(Buffer.from('}'));
  const contents = Buffer.concat(parts);
  if (contents.length > MaxPollCursorDocumentBytes) throw new PollCursorTooLargeError(`encoded document is ${contents.length} bytes`);
  return contents;
}

/**
 * Decode a stored document strictly, as Go's DisallowUnknownFields decoder does: keys match
 * case-insensitively, a repeated object merges into the earlier one, a repeated array reuses its earlier
 * elements, and null leaves a scalar unchanged. Throws PollCursorDocumentInvalidError for any other shape,
 * another schema, an invalid state, or an integer beyond 2^53, which a JavaScript number cannot hold exactly.
 */
export function decodePollCursorDocument(contents: Uint8Array): PollCursorState | undefined {
  let schemaVersion = '';
  let state: PollCursorState | undefined;
  try {
    if (rawJSONKind(contents) !== 'object') throw new InvalidRawJSONError();
    for (const member of rawJSONObjectMembers(contents)) {
      if (isGoFieldKey(member.key, 'schemaVersion')) schemaVersion = decodeGoStringField(member.value, schemaVersion);
      else if (isGoFieldKey(member.key, 'state')) state = decodeNullableObject(member.value, state, emptyPollCursorState, decodeStateMembers);
      else throw new InvalidRawJSONError();
    }
  } catch (error: unknown) { throw new PollCursorDocumentInvalidError(error); }
  if (schemaVersion !== PollCursorSchemaVersion) throw new PollCursorDocumentInvalidError();
  if (state !== undefined) {
    try { validatePollCursorState(state); }
    catch (error: unknown) { throw new PollCursorDocumentInvalidError(error); }
  }
  return state;
}

function emptyPollCursorState(): PollCursorState { return { token: '', seededAtUnixMs: 0, committedAtUnixMs: 0, commitCount: 0, resetCount: 0 }; }

/** Decode an object into current, or a new one; null clears it, as Go does for a pointer field. */
function decodeNullableObject<Value>(raw: Uint8Array, current: Value | undefined, create: () => Value, decodeMembers: (raw: Uint8Array, value: Value) => void): Value | undefined {
  const kind = rawJSONKind(raw);
  if (kind === 'null') return undefined;
  if (kind !== 'object') throw new InvalidRawJSONError();
  const value = current ?? create();
  decodeMembers(raw, value);
  return value;
}

function decodeStateMembers(raw: Uint8Array, state: PollCursorState): void {
  for (const member of rawJSONObjectMembers(raw)) {
    const { key, value } = member;
    if (isGoFieldKey(key, 'token')) state.token = decodeGoStringField(value, state.token);
    else if (isGoFieldKey(key, 'snapshot')) state.snapshot = decodeNullableObject(value, state.snapshot, () => ({ entries: [] }), decodeSnapshotMembers);
    else if (isGoFieldKey(key, 'seededAtUnixMs')) state.seededAtUnixMs = decodeGoInt64Field(value, state.seededAtUnixMs);
    else if (isGoFieldKey(key, 'committedAtUnixMs')) state.committedAtUnixMs = decodeGoInt64Field(value, state.committedAtUnixMs);
    else if (isGoFieldKey(key, 'commitCount')) state.commitCount = decodeGoInt64Field(value, state.commitCount);
    else if (isGoFieldKey(key, 'resetCount')) state.resetCount = decodeGoInt64Field(value, state.resetCount);
    else if (isGoFieldKey(key, 'lastResetAtUnixMs')) state.lastResetAtUnixMs = rawJSONKind(value) === 'null' ? undefined : decodeGoInt64Field(value, 0);
    else throw new InvalidRawJSONError();
  }
}

function decodeSnapshotMembers(raw: Uint8Array, snapshot: KeyedSnapshot): void {
  for (const member of rawJSONObjectMembers(raw)) {
    if (!isGoFieldKey(member.key, 'entries')) throw new InvalidRawJSONError();
    const kind = rawJSONKind(member.value);
    if (kind === 'null') { snapshot.entries = []; continue; }
    if (kind !== 'array') throw new InvalidRawJSONError();
    // Go decodes a repeated array into the earlier elements, so their unset fields survive.
    snapshot.entries = rawJSONArrayElements(member.value).map((element, index) => {
      const entry: KeyedSnapshotEntry = { ...(snapshot.entries[index] ?? { key: '', hash: '', seen: 0 }) };
      const elementKind = rawJSONKind(element);
      if (elementKind === 'null') return entry;
      if (elementKind !== 'object') throw new InvalidRawJSONError();
      decodeEntryMembers(element, entry);
      return entry;
    });
  }
}

function decodeEntryMembers(raw: Uint8Array, entry: KeyedSnapshotEntry): void {
  for (const { key, value } of rawJSONObjectMembers(raw)) {
    if (isGoFieldKey(key, 'key')) entry.key = decodeGoStringField(value, entry.key);
    else if (isGoFieldKey(key, 'hash')) entry.hash = decodeGoStringField(value, entry.hash);
    else if (isGoFieldKey(key, 'seen')) entry.seen = decodeGoInt64Field(value, entry.seen);
    // A json.RawMessage keeps every value's bytes, null included, which validation then refuses.
    else if (isGoFieldKey(key, 'value')) entry.value = new Uint8Array(value);
    else throw new InvalidRawJSONError();
  }
}

/** Decode a Go int64 field: an integer literal, or null to leave it unchanged. */
function decodeGoInt64Field(raw: Uint8Array, current: number): number {
  const kind = rawJSONKind(raw);
  if (kind === 'null') return current;
  const text = Buffer.from(raw).toString('latin1').trim();
  if (kind !== 'number' || !/^-?\d+$/.test(text)) throw new InvalidRawJSONError();
  const value = BigInt(text);
  if (value < -(2n ** 63n) || value >= 2n ** 63n || value < BigInt(Number.MIN_SAFE_INTEGER) || value > BigInt(Number.MAX_SAFE_INTEGER)) throw new InvalidRawJSONError('poll cursor integer is out of range');
  return Number(value);
}

/**
 * Check the storage bounds and snapshot ordering of a state. Throws PollCursorTooLargeError for an oversize
 * token or snapshot, and TypeError for an unsorted or duplicate snapshot key, a malformed hash, a null
 * value, a negative counter, or a number that is not a safe integer.
 */
export function validatePollCursorState(state: PollCursorState): void {
  const tokenBytes = Buffer.byteLength(state.token);
  if (tokenBytes > MaxPollCursorTokenBytes) throw new PollCursorTooLargeError(`token is ${tokenBytes} bytes`);
  const counters = [state.seededAtUnixMs, state.committedAtUnixMs, state.commitCount, state.resetCount, ...(state.lastResetAtUnixMs === undefined ? [] : [state.lastResetAtUnixMs])];
  if (!counters.every(Number.isSafeInteger)) throw new TypeError('poll cursor times and counters must be safe integers');
  if (state.commitCount < 0 || state.resetCount < 0) throw new TypeError('poll cursor counters must not be negative');
  if (state.snapshot !== undefined) validateKeyedSnapshot(state.snapshot);
}

/**
 * Check that entries are sorted by the UTF-8 bytes of unique keys, keys are 1 byte to 1 KiB, hashes are
 * lowercase hex of 16 to 64 characters, values are JSON other than null, and at most
 * MaxKeyedSnapshotEntries entries exist. Throws PollCursorTooLargeError for too many entries and TypeError otherwise.
 */
export function validateKeyedSnapshot(snapshot: KeyedSnapshot): void {
  if (snapshot.entries.length > MaxKeyedSnapshotEntries) throw new PollCursorTooLargeError(`snapshot has ${snapshot.entries.length} entries`);
  let previousKey: Buffer | undefined;
  for (const entry of snapshot.entries) {
    const key = Buffer.from(entry.key);
    if (key.length === 0 || key.length > maxKeyedSnapshotKeyBytes) throw new TypeError('keyed snapshot key must be 1 byte to 1 KiB');
    if (previousKey !== undefined && Buffer.compare(previousKey, key) >= 0) throw new TypeError('keyed snapshot entries must be sorted by unique key');
    previousKey = key;
    if (!/^[0-9a-f]{16,64}$/.test(entry.hash)) throw new TypeError('keyed snapshot hash must be lowercase hex of 16 to 64 characters');
    if (!Number.isSafeInteger(entry.seen) || entry.seen < 0) throw new TypeError('keyed snapshot seen counter must be a non-negative integer');
    if (entry.value !== undefined && !isNonNullRawJSON(entry.value)) throw new TypeError('keyed snapshot value must be JSON other than null');
  }
}

/** Order keyed snapshot keys by their UTF-8 bytes, as Go compares strings. */
export function compareKeyedSnapshotKeys(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left), Buffer.from(right));
}

function isNonNullRawJSON(value: Uint8Array): boolean {
  try { return rawJSONKind(value) !== 'null'; } catch { return false; }
}
