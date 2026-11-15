// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
  ConflictError, InMemoryObjectStore, ObjectNotFoundError, OutcomeUnknownError, PollCursorDocumentInvalidError, PollCursorTooLargeError, ProjectObject,
  MaxKeyedSnapshotEntries, MaxPollCursorTokenBytes, newPollCursorStore, newPollFeedCursorStore, type KeyedSnapshotEntry, type ObjectStore, type PollCursorState,
} from '../src/projectconfig/index.js';
import { decodePollCursorDocument } from '../src/projectconfig/poll_cursor.js';
import { applyKeyedSnapshotChanges, diffKeyedSnapshot, triggerCredentialCallId, type KeyedItem } from '../src/pollingtrigger/index.js';

interface GoEntry { key: string; hash: string; seen: number; value?: unknown }
interface Fixture {
  headKeys: Array<{ name: string; binding?: Record<string, string>; feed?: Record<string, string>; key: string }>;
  encodeCases: Array<{ name: string; state: GoStateInput; document: string; error?: string }>;
  decodeCases: Array<{ name: string; input: string; error: boolean; output?: string }>;
  snapshotCases: Array<{
    name: string; operation: 'diff' | 'apply'; previous: Array<{ key: string; hash: string; seen: number; value?: unknown }> | null;
    items: Array<{ key: string; hash: string; value?: unknown }>; removedKeys?: string[];
    options: { maxEntries: number; keepValues: boolean; evictLeastRecentlySeen: boolean }; error?: string; result?: unknown;
  }>;
  credentialCalls: Array<{ provider: string; connection: string; connectorId: string; triggerName: string; callId: string }>;
}
interface Repeat { text: string; count: number }
interface GoStateInput {
  token: string; tokenRepeat?: Repeat; seededAtUnixMs: number; committedAtUnixMs: number; commitCount: number; resetCount: number; lastResetAtUnixMs: number | null;
  snapshot: Array<{ key: string; keyRepeat?: Repeat; hash: string; seen: number; valueBase64?: string }> | null;
}

// Generated from the Go Connector SDK's sdkgo/pollingtrigger and projectconfig poll cursor code.
const fixture = JSON.parse(readFileSync(new URL('./fixtures/go-poll-cursor-fixtures.json', import.meta.url), 'utf8')) as Fixture;
const scope = { projectId: 'sample-project', kind: 'preview' } as const;
const fixtureKey = { connectorId: 'sample', connectionName: 'main-é-流程 😀', triggerName: 'itemCreated', bindingName: 'main / west' };

function stateFrom(input: GoStateInput): PollCursorState {
  const state: PollCursorState = {
    token: input.tokenRepeat ? input.tokenRepeat.text.repeat(input.tokenRepeat.count) : input.token,
    seededAtUnixMs: input.seededAtUnixMs, committedAtUnixMs: input.committedAtUnixMs, commitCount: input.commitCount, resetCount: input.resetCount,
  };
  if (input.lastResetAtUnixMs !== null) state.lastResetAtUnixMs = input.lastResetAtUnixMs;
  if (input.snapshot !== null) {
    state.snapshot = { entries: input.snapshot.map(entry => {
      const stored: KeyedSnapshotEntry = { key: entry.keyRepeat ? entry.keyRepeat.text.repeat(entry.keyRepeat.count) : entry.key, hash: entry.hash, seen: entry.seen };
      if (entry.valueBase64 !== undefined) stored.value = Buffer.from(entry.valueBase64, 'base64');
      return stored;
    }) };
  }
  return state;
}

/** Records created keys and refuses reads of missing keys as S3 does. */
class KeyRecordingStore implements ObjectStore {
  readonly createdKeys: string[] = [];
  missingKeyReads = 0;
  constructor(readonly inner: ObjectStore = new InMemoryObjectStore()) {}
  async readObject(key: string, version?: string): Promise<ProjectObject> {
    try { return await this.inner.readObject(key, version); }
    catch (error: unknown) {
      if (error instanceof ObjectNotFoundError) { this.missingKeyReads++; throw new Error('access denied'); }
      throw error;
    }
  }
  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> { this.createdKeys.push(key); return this.inner.createObject(key, contents); }
  async compareAndSwapObject(key: string, etag: string, contents: Uint8Array): Promise<ProjectObject> { return this.inner.compareAndSwapObject(key, etag, contents); }
}

async function writeState(state: PollCursorState): Promise<string> {
  const objects = new KeyRecordingStore();
  const store = newPollCursorStore(objects, scope, fixtureKey);
  const record = await store.readPollCursor();
  await store.replacePollCursor(record.revision, state);
  return Buffer.from((await objects.inner.readObject(store.key)).contents).toString('utf8');
}

describe('poll cursor storage matches the Go Connector SDK', () => {
  it('derives every head key as Go does and never reads a missing key', async () => {
    for (const testCase of fixture.headKeys) {
      const objects = new KeyRecordingStore();
      const store = testCase.binding
        ? newPollCursorStore(objects, scope, { connectorId: testCase.binding.ConnectorID!, connectionName: testCase.binding.ConnectionName!, triggerName: testCase.binding.TriggerName!, bindingName: testCase.binding.BindingName! })
        : newPollFeedCursorStore(objects, scope, { connectorId: testCase.feed!.ConnectorID!, connectionName: testCase.feed!.ConnectionName!, feedName: testCase.feed!.FeedName! });
      expect(store.key, testCase.name).toBe(testCase.key);
      expect((await store.readPollCursor()).state).toBeUndefined();
      expect(objects.createdKeys).toEqual([testCase.key]);
      expect(Buffer.from((await objects.inner.readObject(testCase.key)).contents).toString()).toBe('{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null}');
      expect(objects.missingKeyReads).toBe(0);
    }
  });

  it('encodes every state to the bytes Go writes and refuses the states Go refuses', async () => {
    expect(fixture.encodeCases.length).toBeGreaterThan(20);
    for (const testCase of fixture.encodeCases) {
      const written = writeState(stateFrom(testCase.state));
      if (testCase.error === 'tooLarge') await expect(written, testCase.name).rejects.toBeInstanceOf(PollCursorTooLargeError);
      else if (testCase.error) await expect(written, testCase.name).rejects.toBeInstanceOf(TypeError);
      else expect(await written, testCase.name).toBe(testCase.document);
    }
  });

  it('decodes every stored document as Go strict decoding does', async () => {
    expect(fixture.decodeCases.length).toBeGreaterThan(40);
    for (const testCase of fixture.decodeCases) {
      const input = Buffer.from(testCase.input, 'base64');
      if (testCase.error) { expect(() => decodePollCursorDocument(input), testCase.name).toThrow(PollCursorDocumentInvalidError); continue; }
      const state = decodePollCursorDocument(input);
      const encoded = state === undefined ? '{"schemaVersion":"connectors.dex.dev/project-poll-cursor/v1alpha1","state":null}' : await writeState(state);
      expect(encoded, testCase.name).toBe(testCase.output);
    }
  });

  it('diffs and applies keyed snapshots as Go does', () => {
    const toItem = (item: { key: string; hash: string; value?: unknown }): KeyedItem => ({
      snapshotKey: () => item.key, snapshotHash: () => item.hash,
      snapshotValue: () => item.value === undefined ? undefined : Buffer.from(JSON.stringify(item.value)),
    });
    const toEntry = (entry: { key: string; hash: string; seen: number; value?: unknown }): KeyedSnapshotEntry =>
      entry.value === undefined ? { key: entry.key, hash: entry.hash, seen: entry.seen } : { ...entry, value: Buffer.from(JSON.stringify(entry.value)) };
    const toGo = (entry: KeyedSnapshotEntry): GoEntry => entry.value === undefined ? { key: entry.key, hash: entry.hash, seen: entry.seen } : { key: entry.key, hash: entry.hash, seen: entry.seen, value: JSON.parse(Buffer.from(entry.value).toString()) };
    for (const testCase of fixture.snapshotCases) {
      const previous = testCase.previous === null ? undefined : { entries: testCase.previous.map(toEntry) };
      const options = { maxEntries: testCase.options.maxEntries, keepValues: testCase.options.keepValues, evictLeastRecentlySeen: testCase.options.evictLeastRecentlySeen };
      const run = () => {
        if (testCase.operation === 'apply') return { next: applyKeyedSnapshotChanges(previous, testCase.items.map(toItem), testCase.removedKeys ?? [], options).entries.map(toGo) };
        const diff = diffKeyedSnapshot(previous, testCase.items.map(toItem), options);
        return { added: diff.added.map(toGo), changed: diff.changed.map(change => ({ current: toGo(change.current), previous: toGo(change.previous) })), next: diff.next.entries.map(toGo), removed: diff.removed.map(toGo) };
      };
      if (testCase.error === 'tooLarge') expect(run, testCase.name).toThrow(PollCursorTooLargeError);
      else if (testCase.error) expect(() => { try { run(); } catch (error: unknown) { if (error instanceof PollCursorTooLargeError) return; throw error; } }, testCase.name).toThrow();
      else expect(run(), testCase.name).toEqual(testCase.result);
    }
  });

  it('derives the Trigger credential call ID as Go does', () => {
    for (const call of fixture.credentialCalls) {
      expect(triggerCredentialCallId({ provider: call.provider, name: call.connection }, { connectorId: call.connectorId, triggerName: call.triggerName })).toBe(call.callId);
    }
  });
});

/** Applies or drops one compare-and-swap and then reports an unknown outcome. */
class OutcomeUnknownStore extends InMemoryObjectStore {
  constructor(private readonly shouldApply: boolean) { super(); }
  override async compareAndSwapObject(key: string, etag: string, contents: Uint8Array): Promise<ProjectObject> {
    if (this.shouldApply) await super.compareAndSwapObject(key, etag, contents);
    throw new OutcomeUnknownError();
  }
}

describe('poll cursor store', () => {
  const zero = { token: '', seededAtUnixMs: 0, committedAtUnixMs: 0, commitCount: 1, resetCount: 0 };

  it('shares an existing head between stores and detects conflicts', async () => {
    const objects = new InMemoryObjectStore();
    const first = newPollCursorStore(objects, scope, fixtureKey);
    const second = newPollCursorStore(objects, scope, fixtureKey);
    const firstRead = await first.readPollCursor();
    const secondRead = await second.readPollCursor();
    expect(firstRead.revision).toBe(secondRead.revision);
    await first.replacePollCursor(firstRead.revision, { ...zero, token: 'a' });
    await expect(second.replacePollCursor(secondRead.revision, { ...zero, token: 'b' })).rejects.toBeInstanceOf(ConflictError);
    expect((await second.readPollCursor()).state?.token).toBe('a');
  });

  it('reconciles an unknown write outcome by comparing bytes', async () => {
    const applied = newPollCursorStore(new OutcomeUnknownStore(true), scope, fixtureKey);
    const written = await applied.replacePollCursor((await applied.readPollCursor()).revision, { ...zero, token: 'x' });
    expect((await applied.readPollCursor()).revision).toBe(written.revision);
    const lost = newPollCursorStore(new OutcomeUnknownStore(false), scope, fixtureKey);
    await expect(lost.replacePollCursor((await lost.readPollCursor()).revision, { ...zero, token: 'x' })).rejects.toBeInstanceOf(ConflictError);
  });

  it('refuses oversize and malformed states without writing', async () => {
    const store = newPollCursorStore(new InMemoryObjectStore(), scope, fixtureKey);
    const { revision } = await store.readPollCursor();
    const entry = (key: string): KeyedSnapshotEntry => ({ key, hash: '0123456789abcdef', seen: 0 });
    for (const state of [
      { ...zero, token: 't'.repeat(MaxPollCursorTokenBytes + 1) },
      { ...zero, snapshot: { entries: Array.from({ length: MaxKeyedSnapshotEntries + 1 }, (_, index) => entry(`k${index}`)) } },
      { ...zero, snapshot: { entries: [{ ...entry('a'), value: Buffer.from(`"${'v'.repeat(9 << 20)}"`) }] } },
    ]) await expect(store.replacePollCursor(revision, state)).rejects.toBeInstanceOf(PollCursorTooLargeError);
    for (const state of [
      { ...zero, snapshot: { entries: [entry('b'), entry('a')] } },
      { ...zero, commitCount: 1.5 },
      { ...zero, seededAtUnixMs: Number.MAX_SAFE_INTEGER + 2 },
    ]) await expect(store.replacePollCursor(revision, state)).rejects.toBeInstanceOf(TypeError);
    expect((await store.readPollCursor()).state).toBeUndefined();
    const written = await store.replacePollCursor(revision, { ...zero, snapshot: { entries: [] } });
    expect(written.revision).not.toBe('');
  });

  it('holds a document up to 8 MiB in the in-memory store, as Go stores do', async () => {
    const store = newPollCursorStore(new InMemoryObjectStore(), scope, fixtureKey);
    const { revision } = await store.readPollCursor();
    await store.replacePollCursor(revision, { ...zero, snapshot: { entries: [{ key: 'a', hash: '0123456789abcdef', seen: 1, value: Buffer.from(`"${'v'.repeat(5 << 20)}"`) }] } });
    expect((await store.readPollCursor()).state?.snapshot?.entries[0]?.value?.length).toBe((5 << 20) + 2);
  });

  it('rejects an invalid identity', () => {
    const objects = new InMemoryObjectStore();
    for (const key of [
      { ...fixtureKey, connectorId: 'Sample' }, { ...fixtureKey, connectionName: ' main' }, { ...fixtureKey, connectionName: 'main\u0085' },
      { ...fixtureKey, triggerName: 'item-created' }, { ...fixtureKey, bindingName: 'line\nbreak' }, { ...fixtureKey, bindingName: 'é'.repeat(129) },
    ]) expect(() => newPollCursorStore(objects, scope, key), JSON.stringify(key)).toThrow(TypeError);
    expect(() => newPollCursorStore(objects, scope, { ...fixtureKey, bindingName: 'é'.repeat(128) })).not.toThrow();
    expect(() => newPollFeedCursorStore(objects, scope, { connectorId: 'sample', connectionName: 'main', feedName: 'Feed' })).toThrow(TypeError);
  });
});
