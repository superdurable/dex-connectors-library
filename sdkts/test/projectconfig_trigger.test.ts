// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { chmod, mkdtemp, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { z } from 'zod';
import { markTriggerUndeliverable, newTriggerContext, type TriggerEvent } from '../src/index.js';
import {
  ConnectionNotSelectedError, CredentialBrokerClient, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, DexWebObjectStore, InMemoryObjectStore, LoadedProject,
  MaxPendingTriggerEvents, ObjectNotFoundError, OutcomeUnknownError, RelayTokenRenewalDeferredError, RelayedTriggerNotRelayedError, RelaySignatureInvalidError, TriggerInbox,
  TriggerInboxFullError, decodeTriggerConfiguration, encodeTriggerEvent, newDurableTriggerTarget, validateContents, type ObjectStore, type ProjectObject,
} from '../src/projectconfig/index.js';
import { InvalidRawJSONError, compactGoRawJSON, encodeGoJSONValue, encodeGoString, parseTriggerPayloadJSON } from '../src/projectconfig/raw_json.js';
import { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest, verifyRelaySignature } from '../src/relaysignature/index.js';
import { newRecordingLogger } from './trigger_test_support.js';

const scope = { projectId: 'test', kind: 'live' } as const;
const inboxKey = { connectorId: 'slack', connectionName: 'support', triggerName: 'threadReplyCreated', bindingName: 'reply' };
const event = (id: string, text = 'hello <world> & co'): TriggerEvent<{ text: string }> => ({ id, occurredAt: new Date('2025-10-12T00:00:00.120Z'), payload: { text } });

describe('project Trigger inbox', () => {
  it('stores events under the Go key and document format, once per ID, in order', async () => {
    const objects = new InMemoryObjectStore();
    const inbox = new TriggerInbox(objects, scope, inboxKey);
    expect(inbox.key).toBe('projects/test/live/trigger-inboxes/slack/c3VwcG9ydA/threadReplyCreated/cmVwbHk/head');
    await inbox.add({ id: 'Ev1', event: encodeTriggerEvent(event('Ev1')) });
    await inbox.add({ id: 'Ev1', event: encodeTriggerEvent(event('Ev1')) });
    await inbox.add({ id: 'Ev2', event: encodeTriggerEvent(event('Ev2')) });
    expect((await inbox.pending()).map(pending => pending.id)).toEqual(['Ev1', 'Ev2']);
    const stored = Buffer.from((await objects.readObject(inbox.key)).contents).toString('utf8');
    expect(stored.startsWith('{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[{"id":"Ev1","event":{"eventId":"Ev1","occurredAt":"2025-10-12T00:00:00.12Z","payload":{"text":"hello \\u003cworld\\u003e \\u0026 co"}}}')).toBe(true);
    await inbox.remove('Ev1');
    await inbox.remove('absent');
    expect((await inbox.pending()).map(pending => pending.id)).toEqual(['Ev2']);
    expect(() => new TriggerInbox(objects, scope, { ...inboxKey, bindingName: ' padded' })).toThrow();
  });

  it('bounds the backlog and retries a conditional write after a concurrent change or unknown outcome', async () => {
    const objects = new InMemoryObjectStore();
    const inbox = new TriggerInbox(objects, scope, inboxKey);
    for (let index = 0; index < MaxPendingTriggerEvents; index++) await inbox.add({ id: `Ev${index}`, event: Buffer.from('{}') });
    await expect(inbox.add({ id: 'overflow', event: Buffer.from('{}') })).rejects.toBeInstanceOf(TriggerInboxFullError);

    const racing = new InMemoryObjectStore();
    let failures = 1;
    const flaky: ObjectStore = {
      readObject: (key, version) => racing.readObject(key, version),
      createObject: (key, contents) => racing.createObject(key, contents),
      async compareAndSwapObject(key, etag, contents): Promise<ProjectObject> {
        if (failures-- > 0) { await racing.compareAndSwapObject(key, etag, contents); throw new OutcomeUnknownError(); }
        return racing.compareAndSwapObject(key, etag, contents);
      },
    };
    const competing = new TriggerInbox(flaky, scope, inboxKey);
    await competing.add({ id: 'EvA', event: Buffer.from('{}') });
    expect((await competing.pending()).map(pending => pending.id)).toEqual(['EvA']);
  });

  it('persists local Dex Web inboxes beside connections.json across restarts in the Go file layout', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sdkts-local-inbox-'));
    try {
      const path = join(directory, 'connections.json');
      await writeFile(path, JSON.stringify({ schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [] }));
      await chmod(path, 0o600);
      const localScope = { projectId: 'local', kind: 'preview' } as const;
      const first = new DexWebObjectStore(path, localScope);
      await first.initialize();
      const inbox = new TriggerInbox(first, localScope, inboxKey);
      await inbox.add({ id: 'Ev1', event: Buffer.from('{"eventId":"Ev1"}') });
      await inbox.add({ id: 'Ev2', event: Buffer.from('{"eventId":"Ev2"}') });
      const files = await readdir(join(directory, 'trigger-inboxes'));
      expect(files).toHaveLength(1);
      expect(files[0]).toMatch(/^[0-9a-f]{64}-3\.json$/);
      const restarted = new DexWebObjectStore(path, localScope);
      await restarted.initialize();
      expect((await new TriggerInbox(restarted, localScope, inboxKey).pending()).map(pending => pending.id)).toEqual(['Ev1', 'Ev2']);
    } finally { await rm(directory, { recursive: true, force: true }); }
  });

  it('decodes one binding configuration strictly', () => {
    const configuration = { schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2' as const, scope, revision: 1, connections: [], operationConfigurations: [],
      triggerBindings: [{ ...inboxKey, configuration: { channelId: 'C1' } }] };
    expect(decodeTriggerConfiguration(configuration, inboxKey, z.strictObject({ channelId: z.string() }))).toEqual({ channelId: 'C1' });
    expect(() => decodeTriggerConfiguration(configuration, { ...inboxKey, bindingName: 'other' }, z.object({}))).toThrow(ObjectNotFoundError);
    expect(() => decodeTriggerConfiguration(configuration, inboxKey, z.strictObject({}))).toThrow();
  });
});

describe('durable Trigger target', () => {
  it('records before acknowledgement, removes consumed events, and replays pending events after a restart', async () => {
    const objects = new InMemoryObjectStore();
    const delivered: string[] = [];
    const failing = new Set(['Ev2']);
    const target = { handleTrigger: async (_context: unknown, received: TriggerEvent<{ text: string }>) => {
      if (failing.has(received.id)) throw new Error('Dex unavailable');
      if (received.id === 'Ev3') throw markTriggerUndeliverable(new Error('Flow closed'));
      delivered.push(`${received.id}:${received.occurredAt.toISOString()}:${received.payload.text}`);
    } };
    const logger = newRecordingLogger();
    const durable = newDurableTriggerTarget(new TriggerInbox(objects, scope, inboxKey), inboxKey, target, { logger });
    for (const id of ['Ev1', 'Ev2', 'Ev3']) await durable.prepareTrigger!(newTriggerContext(), event(id));
    await durable.handleTrigger(newTriggerContext(), event('Ev1'));
    await expect(durable.handleTrigger(newTriggerContext(), event('Ev2'))).rejects.toThrow('Dex unavailable');
    expect((await new TriggerInbox(objects, scope, inboxKey).pending()).map(pending => pending.id)).toEqual(['Ev2', 'Ev3']);

    failing.clear();
    const restarted = newDurableTriggerTarget(new TriggerInbox(objects, scope, inboxKey), inboxKey, target, { logger, decodePayload: payloadJSON => z.object({ text: z.string() }).parse(parseTriggerPayloadJSON(payloadJSON)) });
    await restarted.replayTriggerDeliveries!(newTriggerContext());
    expect(delivered).toEqual(['Ev1:2025-10-12T00:00:00.120Z:hello <world> & co', 'Ev2:2025-10-12T00:00:00.120Z:hello <world> & co']);
    expect(await new TriggerInbox(objects, scope, inboxKey).pending()).toEqual([]);
    expect(logger.records.find(record => record.message === 'finished replaying pending trigger events')?.attributes).toMatchObject({ delivered: 1, skipped: 1, remaining: 0, binding: 'reply' });
  });

  it('removes an event without invoking the target again after only its removal failed', async () => {
    const backing = new InMemoryObjectStore();
    let isRemovalFailing = false;
    const objects: ObjectStore = {
      readObject: (key, version) => backing.readObject(key, version),
      createObject: (key, contents) => backing.createObject(key, contents),
      compareAndSwapObject: async (key, etag, contents) => { if (isRemovalFailing) throw new Error('storage unavailable'); return backing.compareAndSwapObject(key, etag, contents); },
    };
    let invocations = 0;
    const durable = newDurableTriggerTarget(new TriggerInbox(objects, scope, inboxKey), inboxKey, { handleTrigger: async () => { invocations++; } }, { logger: newRecordingLogger() });
    await durable.prepareTrigger!(newTriggerContext(), event('Ev1'));
    isRemovalFailing = true;
    await expect(durable.handleTrigger(newTriggerContext(), event('Ev1'))).rejects.toThrow('storage unavailable');
    isRemovalFailing = false;
    await durable.handleTrigger(newTriggerContext(), event('Ev1'));
    expect(invocations).toBe(1);
    expect(await new TriggerInbox(objects, scope, inboxKey).pending()).toEqual([]);
  });
});

describe('relay signatures', () => {
  const body = Buffer.from('{"event_id":"Ev1"}');
  const signedAt = new Date(1_760_227_200_000);

  it('signs v1 HMAC-SHA256 over the timestamp and raw body and returns a typed refusal', () => {
    const expected = createHmac('sha256', 'scope-token').update(`v1:1760227200:${body.toString()}`).digest('hex');
    const signature = signRelayedRequest('scope-token', signedAt, body);
    expect(signature).toBe(`v1=${expected}`);
    const headers = { [RelayTimestampHeader.toLowerCase()]: '1760227200', [RelaySignatureHeader.toLowerCase()]: signature };
    expect(verifyRelaySignature('scope-token', headers, body, new Date(signedAt.getTime() + 60_000))).toEqual({ isValid: true, signedAt });
    expect(verifyRelaySignature('other-token', headers, body, signedAt)).toEqual({ isValid: false, refusal: 'mismatch' });
    expect(verifyRelaySignature('scope-token', headers, Buffer.from('{"event_id":"Ev2"}'), signedAt)).toEqual({ isValid: false, refusal: 'mismatch' });
    expect(verifyRelaySignature('scope-token', headers, body, new Date(signedAt.getTime() + 6 * 60_000))).toEqual({ isValid: false, refusal: 'stale' });
    expect(verifyRelaySignature('scope-token', { [RelayTimestampHeader]: '1760227200' }, body, signedAt)).toEqual({ isValid: false, refusal: 'missing' });
    expect(verifyRelaySignature('scope-token', { ...headers, [RelaySignatureHeader.toLowerCase()]: 'v2=00' }, body, signedAt)).toEqual({ isValid: false, refusal: 'malformed' });
    expect(verifyRelaySignature('scope-token', new Headers(headers), body, signedAt).isValid).toBe(true);
  });

  function relayProject(answer: (request: Request) => Response, tokens: string[] = ['scope-token'], now = () => Date.now()) {
    const requests: Array<{ url: string; authorization: string; body: unknown }> = [];
    let tokenReads = 0;
    const broker = new CredentialBrokerClient('https://broker.example.test/internal/connection-credentials/read', async () => tokens[Math.min(tokenReads++, tokens.length - 1)]!, async (url, init) => {
      const request = new Request(String(url), init);
      requests.push({ url: String(url), authorization: request.headers.get('authorization') ?? '', body: JSON.parse(String(init?.body)) });
      return answer(request);
    }, now);
    const configuration = {
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3' as const, scope, revision: 1, triggerBindings: [], operationConfigurations: [],
      connections: [
        { connectorId: 'slack', connectionName: 'support', modulePath: 'slack', provider: 'slack', sharedConnectionId: 'conn-slack', configuration: {} },
        { connectorId: 'slack', connectionName: 'private', modulePath: 'slack', provider: 'slack', configuration: {} },
      ],
      credentialBroker: { endpoint: 'https://broker.example.test/internal/connection-credentials/read', tokenSecretRef: { key: `projects/test/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/${'a'.repeat(32)}`, version: 'v1', digest: `sha256:${'b'.repeat(64)}` } },
    };
    return { project: new LoadedProject(configuration, undefined, new InMemoryObjectStore(), undefined, broker), requests, tokenReads: () => tokenReads };
  }

  it('registers a shared connection on the broker origin and maps every answer', async () => {
    const statuses = ['REGISTERED', 'CONNECTION_NOT_SELECTED', 'NOT_RELAYED', 'SURPRISE'];
    const { project, requests } = relayProject(() => Response.json({ status: statuses.shift() }));
    await project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, '/internal/slack-triggers/register');
    expect(requests[0]).toEqual({ url: 'https://broker.example.test/internal/slack-triggers/register', authorization: 'Bearer scope-token', body: { sharedConnectionId: 'conn-slack', connectionName: 'support' } });
    await expect(project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, '/internal/slack-triggers/register')).rejects.toBeInstanceOf(ConnectionNotSelectedError);
    await expect(project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, '/internal/slack-triggers/register')).rejects.toBeInstanceOf(RelayedTriggerNotRelayedError);
    await expect(project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, '/internal/slack-triggers/register')).rejects.toBeInstanceOf(CredentialBrokerRejectedError);
    await expect(project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'private' }, '/internal/slack-triggers/register')).rejects.toThrow('shared connection entry');
    await expect(project.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, 'https://elsewhere.test/x')).rejects.toThrow('absolute path');
    const unavailable = relayProject(() => new Response('', { status: 503 })).project;
    await expect(unavailable.registerRelayedTrigger({ connectorId: 'slack', connectionName: 'support' }, '/internal/slack-triggers/register')).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
  });

  it('verifies relayed requests with the scope token, rereading it after a mismatch at most every 30 seconds', async () => {
    let now = 1_000_000;
    const { project, tokenReads } = relayProject(() => Response.json({}), ['old-token', 'rotated-token', 'rotated-token'], () => now);
    const signed = (token: string) => ({ [RelayTimestampHeader]: String(Math.floor(Date.now() / 1000)), [RelaySignatureHeader]: signRelayedRequest(token, new Date(), body) });
    await project.verifyRelayedRequest(signed('old-token'), body, new Date());
    await expect(project.verifyRelayedRequest({}, body, new Date())).rejects.toBeInstanceOf(RelaySignatureInvalidError);
    expect(tokenReads()).toBe(1);
    await project.verifyRelayedRequest(signed('rotated-token'), body, new Date());
    expect(tokenReads()).toBe(2);
    // Inside the 30-second window a mismatch cannot reread the token, so it is retryable rather than refused.
    await expect(project.verifyRelayedRequest(signed('forged-token'), body, new Date())).rejects.toBeInstanceOf(RelayTokenRenewalDeferredError);
    await expect(project.verifyRelayedRequest(signed('forged-token'), body, new Date())).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
    expect(tokenReads()).toBe(2);
    now += 31_000;
    await expect(project.verifyRelayedRequest(signed('forged-token'), body, new Date())).rejects.toBeInstanceOf(RelaySignatureInvalidError);
    expect(tokenReads()).toBe(3);
    const failing = new CredentialBrokerClient('https://broker.example.test/read', async () => { throw new Error('SENTINEL token store unavailable'); });
    await expect(failing.verifyRelayedRequest(signed('old-token'), body, new Date())).rejects.toBeInstanceOf(CredentialBrokerUnavailableError);
  });

  it('keeps serving reads and valid relay requests with the cached token while a forged request rereads the token', async () => {
    let renewals = 0;
    const requests: string[] = [];
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async renew => {
      if (!renew) return 'scope-token';
      renewals++;
      return new Promise<string>(() => undefined);
    }, async (url, init) => {
      requests.push(String(url));
      return Response.json({ status: 'READY', credential: { accessToken: 'private' }, revision: 1, echo: String(init?.body) });
    });
    const signed = (token: string) => ({ [RelayTimestampHeader]: String(Math.floor(Date.now() / 1000)), [RelaySignatureHeader]: signRelayedRequest(token, new Date(), body) });
    await broker.verifyRelayedRequest(signed('scope-token'), body, new Date());
    // This forged request's token reread never settles while it holds the token lock.
    const forged = broker.verifyRelayedRequest(signed('forged-token'), body, new Date()).catch((error: unknown) => error);
    await expect.poll(() => renewals).toBe(1);
    const started = Date.now();
    expect((await broker.read('host-selection')).credential).toEqual({ accessToken: 'private' });
    await broker.verifyRelayedRequest(signed('scope-token'), body, new Date());
    expect(Date.now() - started).toBeLessThan(1_000);
    expect(requests).toEqual(['https://broker.example.test/read']);
    void forged;
  });

  it('shares one token read among concurrent requests and accepts a rotation that forged requests would otherwise delay', async () => {
    let now = 1_000_000;
    let tokenReads = 0;
    let currentToken = 'old-token';
    const broker = new CredentialBrokerClient('https://broker.example.test/read', async () => {
      tokenReads++;
      await new Promise(resolve => setTimeout(resolve, 5));
      return currentToken;
    }, async () => Response.json({}), () => now);
    const signed = (token: string) => ({ [RelayTimestampHeader]: String(Math.floor(Date.now() / 1000)), [RelaySignatureHeader]: signRelayedRequest(token, new Date(), body) });
    const forged = await Promise.allSettled(Array.from({ length: 20 }, () => broker.verifyRelayedRequest(signed('forged-token'), body, new Date())));
    expect(forged.every(result => result.status === 'rejected')).toBe(true);
    expect(tokenReads).toBe(2);
    currentToken = 'rotated-token';
    // A forged request consumed the window, so the rotated signature is retryable now and accepted after it.
    await expect(broker.verifyRelayedRequest(signed('rotated-token'), body, new Date())).rejects.toBeInstanceOf(RelayTokenRenewalDeferredError);
    now += 30_000;
    await broker.verifyRelayedRequest(signed('rotated-token'), body, new Date());
    expect(tokenReads).toBe(3);
    await expect(broker.verifyRelayedRequest({ [RelayTimestampHeader]: '1', [RelaySignatureHeader]: signRelayedRequest('forged-token', new Date(1000), body) }, body, new Date())).rejects.toBeInstanceOf(RelaySignatureInvalidError);
    expect(tokenReads).toBe(3);
  });
});

describe('Go-written Trigger inboxes', () => {
  const fixture = (name: string) => readFileSync(join(import.meta.dirname, 'fixtures', name));
  const goKey = { connectorId: 'slack', connectionName: 'support', triggerName: 'threadReplyCreated', bindingName: 'reply' };
  const goScope = { projectId: 'fixture', kind: 'live' } as const;

  async function inboxWith(contents: Buffer) {
    const objects = new InMemoryObjectStore();
    const inbox = new TriggerInbox(objects, goScope, goKey);
    expect(inbox.key).toBe('projects/fixture/live/trigger-inboxes/slack/c3VwcG9ydA/threadReplyCreated/cmVwbHk/head');
    await objects.createObject(inbox.key, contents);
    return { objects, inbox };
  }

  it('keeps every event byte-for-byte and rewrites the document exactly as Go does', async () => {
    const { objects, inbox } = await inboxWith(fixture('go-trigger-inbox-three-events.json'));
    const pending = await inbox.pending();
    expect(pending.map(event => event.id)).toEqual(['Ev1<&>', 'Ev2', 'Ev3']);
    expect(Buffer.from(pending[1]!.event).toString()).toContain('"alpha":9007199254740993,"list":[1.50,"x"],"nested":{"a":null,"b":true},"zeta":1');
    const goFourEvents = fixture('go-trigger-inbox-four-events.json');
    const added = (await (await inboxWith(goFourEvents)).inbox.pending())[3]!;
    await inbox.add(added);
    expect(Buffer.from((await objects.readObject(inbox.key)).contents).equals(goFourEvents)).toBe(true);
    await inbox.remove('Ev2');
    expect(Buffer.from((await objects.readObject(inbox.key)).contents).equals(fixture('go-trigger-inbox-after-remove.json'))).toBe(true);
  });

  it('decodes payloads like Go into any by default and hands the raw payload to a custom decoder', async () => {
    const defaults = await inboxWith(fixture('go-trigger-inbox-three-events.json'));
    const delivered: Array<TriggerEvent<unknown>> = [];
    const record = { handleTrigger: async (_context: unknown, event: TriggerEvent<unknown>) => { delivered.push(event); } };
    await newDurableTriggerTarget(defaults.inbox, goKey, record, { logger: newRecordingLogger() }).replayTriggerDeliveries!(newTriggerContext());
    expect(delivered.map(event => event.id)).toEqual(['Ev1<&>', 'Ev2', 'Ev3']);
    expect(delivered[0]!.occurredAt.toISOString()).toBe('2025-10-12T00:00:00.123Z');
    expect((delivered[0]!.payload as { text: string }).text).toBe('<b>approve</b> & ship \u2028 ünïcode "quoted"\n\b\f\u0001');
    // The default has Go's float64 precision: 9007199254740993 arrives as the nearest double.
    expect(delivered[2]!.payload).toEqual({ count: 9007199254740992, label: 'big' });
    expect(await defaults.inbox.pending()).toEqual([]);

    const exact = await inboxWith(fixture('go-trigger-inbox-three-events.json'));
    const payloads: string[] = [];
    const keepExactIntegers = (payloadJSON: string) => { payloads.push(payloadJSON); return JSON.parse(payloadJSON, (_key, value: unknown, context?: { source?: string }) => typeof value === 'number' && !Number.isSafeInteger(value) && /^-?\d+$/.test(context?.source ?? '') ? BigInt(context!.source!) : value) as unknown; };
    const exactEvents: Array<TriggerEvent<unknown>> = [];
    await newDurableTriggerTarget(exact.inbox, goKey, { handleTrigger: async (_context, event) => { exactEvents.push(event); } }, { logger: newRecordingLogger(), decodePayload: keepExactIntegers }).replayTriggerDeliveries!(newTriggerContext());
    expect(payloads.slice(1)).toEqual(['{"alpha":9007199254740993,"list":[1.50,"x"],"nested":{"a":null,"b":true},"zeta":1}', '{"count":9007199254740993,"label":"big"}']);
    expect(exactEvents[2]!.payload).toEqual({ count: 9007199254740993n, label: 'big' });
    expect(await exact.inbox.pending()).toEqual([]);
  });

  it('parses payload numbers as Go decodes them into any, refusing only overflow', () => {
    for (const refused of ['1e400', '-1e400', '[1.5e400]', '{"a":2e300000}']) expect(() => parseTriggerPayloadJSON(refused), refused).toThrow('outside the double range');
    for (const [literal, expected] of [['9007199254740993', 9007199254740992], ['9007199254740993e0', 9007199254740992], ['0.1', 0.1], ['1e-400', 0], ['-1e-400', -0], ['4e-325', 0],
      ['5e-324', 5e-324], ['1e+23', 1e23], ['1.7976931348623157e+308', Number.MAX_VALUE], ['123456789012345680000', 123456789012345680000], ['-0.0', -0]] as const)
      expect(parseTriggerPayloadJSON(literal), literal).toBe(expected);
    expect(() => parseTriggerPayloadJSON('[1,]')).toThrow(InvalidRawJSONError);
  });

  it('decodes payload strings and keys with Go rules and gives a custom decoder the exact bytes', async () => {
    // Expected values come from Go's json.Unmarshal of the same bytes into any.
    const raw = Buffer.concat([Buffer.from('{"k\\ud800":"x\\ud800y","t":"a'), Buffer.from([0xe2, 0x82]), Buffer.from('z","pair":"\\ud83d\\ude00","low":"\\udc00","s":["\\ud800\\u0041"]}')]);
    expect(parseTriggerPayloadJSON(raw)).toEqual({ 'k\ufffd': 'x\ufffdy', t: 'a\ufffd\ufffdz', pair: '\u{1f600}', low: '\ufffd', s: ['\ufffdA'] });

    const objects = new InMemoryObjectStore();
    const inbox = new TriggerInbox(objects, scope, inboxKey);
    const event = Buffer.concat([Buffer.from('{"eventId":"Ev1","occurredAt":"2025-10-12T00:00:00Z","payload":{"t":"a'), Buffer.from([0xff]), Buffer.from('b"}}')]);
    await inbox.add({ id: 'Ev1', event });
    const received: Array<{ text: string; bytes: string; payload: unknown }> = [];
    const replay = (decodePayload?: (payloadJSON: string, payloadBytes: Uint8Array) => unknown) => newDurableTriggerTarget(inbox, inboxKey, { handleTrigger: async (_context, delivered) => { received.push({ text: '', bytes: '', payload: delivered.payload }); } }, {
      logger: newRecordingLogger(), ...(decodePayload ? { decodePayload } : {}),
    }).replayTriggerDeliveries!(newTriggerContext());
    await replay((payloadJSON, payloadBytes) => { received.push({ text: payloadJSON, bytes: Buffer.from(payloadBytes).toString('hex'), payload: undefined }); return parseTriggerPayloadJSON(payloadBytes); });
    expect(received[0]!.bytes).toBe(Buffer.from('{"t":"a\xffb"}', 'latin1').toString('hex'));
    expect(received[1]!.payload).toEqual({ t: 'a\ufffdb' });

    // A decoder that changes its bytes leaves the inbox's stored event unchanged.
    const mutable = new TriggerInbox(new InMemoryObjectStore(), scope, inboxKey);
    await mutable.add({ id: 'EvMutated', event: Buffer.from('{"eventId":"EvMutated","occurredAt":"2025-10-12T00:00:00Z","payload":{"n":1}}') });
    const stored = Buffer.from((await mutable.pending())[0]!.event).toString();
    await expect(newDurableTriggerTarget(mutable, inboxKey, { handleTrigger: async () => { throw new Error('retry'); } }, {
      logger: newRecordingLogger(), decodePayload: (_payloadJSON, payloadBytes) => { payloadBytes.fill(0x39); throw new Error('decoder failed'); },
    }).replayTriggerDeliveries!(newTriggerContext())).rejects.toThrow('decode pending Trigger event');
    expect(Buffer.from((await mutable.pending())[0]!.event).toString()).toBe(stored);
  });

  it('reads and updates a 6 MiB inbox, as Go allows up to 8 MiB outside S3', { timeout: 60_000 }, async () => {
    const objects = new InMemoryObjectStore();
    const inbox = new TriggerInbox(objects, goScope, goKey);
    const large = 'x'.repeat(6 << 20);
    await inbox.add({ id: 'EvLarge', event: Buffer.from(`{"eventId":"EvLarge","occurredAt":"2025-10-12T00:00:00Z","payload":{"text":"${large}"}}`) });
    await inbox.add({ id: 'EvSmall', event: Buffer.from('{"eventId":"EvSmall","occurredAt":"2025-10-12T00:00:00Z","payload":{}}') });
    expect((await objects.readObject(inbox.key)).contents.length).toBeGreaterThan(6 << 20);
    await inbox.remove('EvLarge');
    expect((await inbox.pending()).map(event => event.id)).toEqual(['EvSmall']);
    await expect(inbox.add({ id: 'EvHuge', event: Buffer.from(`{"text":"${'y'.repeat(9 << 20)}"}`) })).rejects.toBeInstanceOf(TriggerInboxFullError);
    expect(() => validateContents(new Uint8Array(5 << 20))).toThrow();
  });

  it('refuses documents that Go would refuse', async () => {
    for (const invalid of ['{"schemaVersion":"other","events":[]}', '{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[],"extra":1}',
      '{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[{"id":"Ev1","event":{},"extra":1}]}', '{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":[]} {}']) {
      await expect((await inboxWith(Buffer.from(invalid))).inbox.pending(), invalid).rejects.toThrow('trigger inbox document is invalid');
    }
    expect(await (await inboxWith(Buffer.from('{"schemaVersion":"connectors.dex.dev/project-trigger-inbox/v1alpha1","events":null}'))).inbox.pending()).toEqual([]);
    expect(compactGoRawJSON(Buffer.from('{ "b" : [1.0e3, "<x>"] }')).toString()).toBe('{"b":[1.0e3,"\\u003cx\\u003e"]}');
  });

  it('decodes and rewrites every Go decoding case exactly as the Go SDK does', async () => {
    const cases = JSON.parse(fixture('go-trigger-inbox-decoding-cases.json').toString()) as Array<{ name: string; input: string; error: boolean; ids?: string[]; output?: string }>;
    expect(cases.length).toBeGreaterThan(8);
    for (const testCase of cases) {
      const { objects, inbox } = await inboxWith(Buffer.from(testCase.input, 'base64'));
      if (testCase.error) { await expect(inbox.pending(), testCase.name).rejects.toThrow('trigger inbox document is invalid'); continue; }
      expect((await inbox.pending()).map(event => event.id), testCase.name).toEqual(testCase.ids);
      await inbox.add({ id: 'EvAdded', event: Buffer.from('{"added":true}') });
      expect(Buffer.from((await objects.readObject(inbox.key)).contents).toString('base64'), testCase.name).toBe(testCase.output);
    }
  });

  it('encodes new payloads exactly as Go marshals the equivalent map, slice, string, and float64 values', () => {
    const cases = new Map((JSON.parse(fixture('go-json-encoding-cases.json').toString()) as Array<{ name: string; output: string }>).map(testCase => [testCase.name, Buffer.from(testCase.output, 'base64').toString()]));
    const values: Record<string, unknown> = {
      'sorted map keys': { zeta: 1, alpha: { b: true, a: null }, 'é': 'accent', Z: 'upper', a: ['x', 2.5] },
      floats: [1e21, 1.5e-7, 0.1, -0, 100, 1e-6, 123456789012345680000, 1e20, -2.5e-10, 5e-324],
      strings: { html: '<b>&</b>', separators: '\u2028\u2029', controls: '\b\f\n\r\t\u0001\u001f\u007f', emoji: '😀', quote: '"\\/' },
      'nested arrays': [[], {}, [{ y: 2, x: 1 }]],
      'faithful floats': [1e23, 1e21, Number.MAX_VALUE, 5e-324, 123456789012345680000, 2e300, 1e22, 0.1, -1e23],
    };
    for (const [name, value] of Object.entries(values)) expect(encodeGoJSONValue(value), name).toBe(cases.get(name));
    // Every float Go writes parses back unchanged, so a Go-written event never sticks in the inbox.
    expect(parseTriggerPayloadJSON(cases.get('faithful floats')!)).toEqual(values['faithful floats']);
    expect(encodeGoJSONValue({ big: 9007199254740993n, when: new Date('2025-10-12T00:00:00.120Z'), skipped: undefined })).toBe('{"big":9007199254740993,"when":"2025-10-12T00:00:00.12Z"}');
    for (const refused of [Number.NaN, Infinity, [undefined], () => 1, new Map(), Symbol('s')]) expect(() => encodeGoJSONValue(refused), String(refused)).toThrow();
    const cycle: Record<string, unknown> = {};
    cycle.self = cycle;
    expect(() => encodeGoJSONValue(cycle)).toThrow('cycle');
    expect(encodeGoString('a\ud800b')).toBe('"a\ufffdb"');
    // eslint-disable-next-line no-sparse-arrays -- the holes are the case under test.
    for (const sparse of [[1, , 3], [, 1], new Array(2)]) expect(() => encodeGoJSONValue(sparse), String(sparse.length)).toThrow('array hole');
    // eslint-disable-next-line no-sparse-arrays -- the hole is the case under test.
    const inherited: unknown[] = [1, , 3];
    Object.setPrototypeOf(inherited, Object.assign(Object.create(Array.prototype) as object, { 1: 'inherited' }));
    expect(1 in inherited).toBe(true);
    expect(() => encodeGoJSONValue(inherited)).toThrow('array hole');
    const selfReturning = { toJSON() { return selfReturning; } };
    expect(() => encodeGoJSONValue({ value: selfReturning })).toThrow('cycle');
    expect(encodeGoJSONValue({ value: { toJSON: () => ({ kept: 1 }) } })).toBe('{"value":{"kept":1}}');
  });

  it('encodes and scans nesting up to Go\'s 10000 levels without recursion and refuses deeper values', () => {
    const nested = (depth: number): unknown => { let value: unknown = []; for (let level = 1; level < depth; level++) value = [value]; return value; };
    for (const depth of [9999, 10_000]) {
      const encoded = encodeGoJSONValue(nested(depth));
      expect(encoded.length).toBe(depth * 2);
      expect(compactGoRawJSON(Buffer.from(encoded)).length).toBe(depth * 2);
    }
    expect(() => encodeGoJSONValue(nested(10_001))).toThrow('nested too deeply');
    expect(() => compactGoRawJSON(Buffer.from(`${'['.repeat(10_001)}${']'.repeat(10_001)}`))).toThrow(InvalidRawJSONError);
  });
});
