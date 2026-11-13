// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Server } from 'node:http';
import { SecretString, markTriggerUndeliverable, newTriggerContext, type TriggerTarget } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, describe, expect, it } from 'vitest';
import { SlackClient } from '../src/client.js';
import {
  ChannelThreadCreatedTriggerConfigurationSchema, ThreadReplyCreatedTriggerConfigurationSchema, defineThreadReplyCreatedTriggerBinding, newConnection, newMessageTriggerRunner,
  newProjectMessageTriggerRunner, newThreadReplyCreatedTrigger, relayedTriggerPath, type MessageEvent, type SlackSocketDialer,
} from '../src/index.js';
import {
  FakeSocket, RecordingRoutes, RelayHost, eventsEnvelope, eventsPayload, newRecordingLogger, newSlackSender, postRelayed, relayScopeToken, replyMessage, rootMessage, sentinelText, serveRunner,
} from './trigger_support.js';

const timings = { reconnectDelayMs: 2, reconnectMaxDelayMs: 8, readTimeoutMs: 2_000 };
const servers: Server[] = [];
const stops: AbortController[] = [];
afterEach(async () => {
  for (const stop of stops.splice(0)) stop.abort();
  await Promise.all(servers.splice(0).map(server => new Promise(resolve => server.close(resolve))));
});

function newSocketClient(sockets: FakeSocket[], logger = newRecordingLogger(), appToken = 'xapp-SENTINEL-APP', socketModeDeliveryQueueCapacity?: number) {
  const dialed: string[] = [];
  const socketDialer: SlackSocketDialer = async url => {
    dialed.push(url);
    const socket = sockets.shift();
    if (!socket) throw new Error(`no socket for ${url}`);
    return socket;
  };
  const credentials = { botToken: new SecretString('xoxb-SENTINEL-BOT'), userToken: new SecretString('xoxp-SENTINEL-USER'), appToken: new SecretString(appToken) };
  const sender = newSlackSender();
  const client = new SlackClient({ endpoint: 'http://127.0.0.1:9/api' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: sender.send, socketDialer, logger, triggerTimings: timings, ...(socketModeDeliveryQueueCapacity ? { socketModeDeliveryQueueCapacity } : {}) });
  return { connection: newConnection(client, { provider: 'slack', name: 'workspace' }), logger, dialed, sender };
}

function run(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  stops.push(stop);
  return runner.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
}

describe('Slack message matching', () => {
  it('matches roots and replies by channel, poster, text, and thread position, ignoring bots and subtypes', async () => {
    const socket = new FakeSocket();
    const { connection, logger } = newSocketClient([socket]);
    const routes = new RecordingRoutes();
    run(newMessageTriggerRunner({ connection,
      channelThreadCreatedRoutes: [{ bindingName: 'start', target: routes.target('root'), configuration: { channelId: 'C1', threadTriggerMatcher: { messageContains: 'REQUEST', posterUserIds: ['U1'] } } }],
      threadReplyCreatedRoutes: [{ bindingName: 'reply', target: routes.target('reply'), configuration: { channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U2'] } } }],
    }));
    const envelopes = [
      eventsEnvelope('e1', 'EvRoot', rootMessage('1.0')),
      eventsEnvelope('e2', 'EvOtherChannel', rootMessage('2.0', { channel: 'C9' })),
      eventsEnvelope('e3', 'EvOtherPoster', rootMessage('3.0', { user: 'U3' })),
      eventsEnvelope('e4', 'EvNoText', rootMessage('4.0', { text: 'hello' })),
      eventsEnvelope('e5', 'EvBot', rootMessage('5.0', { bot_id: 'B1' })),
      eventsEnvelope('e6', 'EvEdited', rootMessage('6.0', { subtype: 'message_changed' })),
      eventsEnvelope('e7', 'EvReply', replyMessage('1.1', '1.0')),
      eventsEnvelope('e8', 'EvReplyByOther', replyMessage('1.2', '1.0', { user: 'U3' })),
      eventsEnvelope('e9', 'EvBroadcastRoot', replyMessage('1.0', '1.0')),
      { envelope_id: 'e10', type: 'slash_commands', payload: {} },
      { type: 'hello' },
    ];
    for (const envelope of envelopes) socket.push(envelope);
    await expect.poll(() => socket.acknowledged.length).toBe(10);
    expect(routes.deliveries).toEqual(['root:EvRoot', 'reply:EvReply']);
    expect(routes.events.get('EvRoot')).toEqual({ id: 'EvRoot', occurredAt: new Date('2025-10-12T00:00:00Z'),
      payload: { teamId: 'T1', channelId: 'C1', timestamp: '1.0', threadTimestamp: '1.0', userId: 'U1', text: `${sentinelText} request approval` } });
    expect(routes.events.get('EvReply')?.payload.threadTimestamp).toBe('1.0');
    const reasons = logger.find('trigger event ignored').map(record => `${record.attributes.binding}:${record.attributes.event_id}:${record.attributes.reason}`);
    expect(reasons).toEqual(expect.arrayContaining([
      'start:EvOtherChannel:channel_mismatch', 'start:EvOtherPoster:matcher_mismatch', 'start:EvNoText:matcher_mismatch', 'start:EvBot:bot', 'start:EvEdited:subtype',
      'reply:EvRoot:not_a_reply', 'start:EvReply:not_a_root', 'reply:EvReplyByOther:matcher_mismatch', 'reply:EvBroadcastRoot:not_a_reply',
    ]));
    expect(logger.find('trigger event ignored', { reason: 'subtype' })[0]?.attributes.subtype).toBe('message_changed');
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });

  it('validates configurations as Go does', () => {
    expect(ThreadReplyCreatedTriggerConfigurationSchema.safeParse({ channelId: 'C1', threadReplyMatcher: {} }).success).toBe(false);
    expect(ThreadReplyCreatedTriggerConfigurationSchema.safeParse({ channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U1', 'U1'] } }).success).toBe(false);
    expect(ChannelThreadCreatedTriggerConfigurationSchema.safeParse({ channelId: ' ' }).success).toBe(false);
    expect(ChannelThreadCreatedTriggerConfigurationSchema.safeParse({ channelId: 'C1' }).success).toBe(true);
    expect(ChannelThreadCreatedTriggerConfigurationSchema.safeParse({ channelId: 'C1', extra: true }).success).toBe(false);
    const binding = defineThreadReplyCreatedTriggerBinding({ connectionName: 'workspace', bindingName: 'reply', configurationUI: { units: [
      { id: 'approvers', unitId: 'memberPicker', label: 'Approvers', description: 'Select approvers.', required: true, bindings: [{ port: 'memberIds', jsonPointer: '/threadReplyMatcher/posterUserIds' }] },
    ] } });
    expect(binding.definition.trigger).toEqual({ connectorId: 'slack', triggerName: 'threadReplyCreated' });
  });
});

describe('Slack Socket Mode', () => {
  it('reconnects after a failure and a requested disconnect, acknowledges, and delivers', async () => {
    const [failing, refreshed, healthy] = [new FakeSocket(), new FakeSocket(), new FakeSocket()];
    const { connection, logger, dialed, sender } = newSocketClient([failing, refreshed, healthy]);
    const routes = new RecordingRoutes();
    const result = run(newThreadReplyCreatedTrigger({ connection, connectionName: 'workspace', bindingName: 'reply', target: routes.target('reply'), configuration: { channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U2'] } } }));
    failing.fail('connection reset');
    refreshed.push({ type: 'hello' });
    refreshed.push({ type: 'disconnect', reason: 'refresh_requested' });
    healthy.push(eventsEnvelope('e1', 'EvReply', replyMessage('1.1', '1.0')));
    await expect.poll(() => routes.deliveries).toEqual(['reply:EvReply']);
    expect(healthy.acknowledged).toEqual(['e1']);
    expect(dialed).toHaveLength(3);
    expect(sender.requests[0]?.headers?.Authorization).toBe('Bearer xapp-SENTINEL-APP');
    expect(logger.find('slack socket mode connection failed; reconnecting', { attempt: 1, error: 'connection reset' })).toHaveLength(1);
    expect(logger.find('slack socket mode disconnected; reconnecting', { reason: 'refresh_requested' })).toHaveLength(1);
    expect(logger.find('slack socket mode connected', { reconnect: true })).toHaveLength(2);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
    stops.at(-1)!.abort();
    expect(await result).toMatchObject({ name: 'TriggerDeliveryAbortedError' });
  });

  it('acknowledges before a durable target completes and delivers a pending root before a reply after reconnect', async () => {
    const [first, second] = [new FakeSocket(), new FakeSocket()];
    const { connection } = newSocketClient([first, second]);
    const objects = new InMemoryObjectStore();
    const scope = { projectId: 'p', kind: 'live' } as const;
    const deliveries: string[] = [];
    let isRootBlocked = true;
    const durable = (bindingName: string, triggerName: string, kind: string): TriggerTarget<MessageEvent> => {
      const key = { connectorId: 'slack', connectionName: 'workspace', triggerName, bindingName };
      return newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, { handleTrigger: async (_context, event) => {
        if (kind === 'root' && isRootBlocked) throw new Error('Worker stopped');
        deliveries.push(`${kind}:${event.id}`);
      } }, { logger: newRecordingLogger() });
    };
    run(newMessageTriggerRunner({ connection,
      channelThreadCreatedRoutes: [{ bindingName: 'start', target: durable('start', 'channelThreadCreated', 'root'), configuration: { channelId: 'C1' } }],
      threadReplyCreatedRoutes: [{ bindingName: 'reply', target: durable('reply', 'threadReplyCreated', 'reply'), configuration: { channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U2'] } } }],
    }));
    first.push(eventsEnvelope('e1', 'EvRoot', rootMessage('1.0')));
    first.push(eventsEnvelope('e2', 'EvReply', replyMessage('1.1', '1.0')));
    await expect.poll(() => first.acknowledged).toEqual(['e1', 'e2']);
    expect(deliveries).toEqual([]);
    isRootBlocked = false;
    await expect.poll(() => deliveries, { timeout: 5_000 }).toEqual(['root:EvRoot', 'reply:EvReply']);
  });

  it('closes the socket and returns when the run stops while Slack is dialing', async () => {
    let releaseDial!: () => void;
    const dialReleased = new Promise<void>(resolve => { releaseDial = resolve; });
    let isDialing = false;
    let isClosed = false;
    // The socket never times out, as Slack's pings keep a quiet connection alive, and ends only when closed.
    let wakeRead: () => void = () => undefined;
    const pingedSocket = {
      readMessage: () => new Promise<unknown>((_resolve, reject) => { wakeRead = () => reject(new Error('connection closed')); if (isClosed) wakeRead(); }),
      writeMessage: async () => undefined,
      close: () => { isClosed = true; wakeRead(); },
    };
    const credentials = { botToken: new SecretString('b'), userToken: new SecretString('u'), appToken: new SecretString('xapp-SENTINEL') };
    const client = new SlackClient({ endpoint: 'http://127.0.0.1:9/api' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, {
      send: newSlackSender().send, logger: newRecordingLogger(), triggerTimings: timings,
      // This dialer ignores the abort and resolves only after it, as a dial that completes during shutdown does.
      socketDialer: async () => { isDialing = true; await dialReleased; return pingedSocket; },
    });
    const stop = new AbortController();
    const result = newMessageTriggerRunner({ connection: newConnection(client, { provider: 'slack', name: 'workspace' }),
      channelThreadCreatedRoutes: [{ bindingName: 'start', target: new RecordingRoutes().target('root'), configuration: { channelId: 'C1' } }] }).run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    await expect.poll(() => isDialing).toBe(true);
    stop.abort();
    releaseDial();
    expect(await result).toMatchObject({ name: 'TriggerDeliveryAbortedError' });
    expect(isClosed).toBe(true);
  });

  it('stops reading Socket Mode envelopes at the durable delivery queue bound and delivers all in order after release', async () => {
    const socket = new FakeSocket();
    const capacity = 3;
    const { connection } = newSocketClient([socket], newRecordingLogger(), 'xapp-SENTINEL-APP', capacity);
    let reads = 0;
    const readMessage = socket.readMessage.bind(socket);
    socket.readMessage = async readTimeoutMs => { reads++; return readMessage(readTimeoutMs); };
    const objects = new InMemoryObjectStore();
    const delivered: string[] = [];
    let releaseStuck!: () => void;
    const stuckReleased = new Promise<void>(resolve => { releaseStuck = resolve; });
    let isStuckStarted = false;
    // The flood uses its own binding, because a durable inbox holds its lock during a delivery attempt.
    const route = (bindingName: string, text: string) => {
      const key = { connectorId: 'slack', connectionName: 'workspace', triggerName: 'channelThreadCreated', bindingName };
      return { bindingName, configuration: { channelId: 'C1', threadTriggerMatcher: { messageContains: text } }, target: newDurableTriggerTarget(new TriggerInbox(objects, { projectId: 'p', kind: 'live' }, key), key, {
        handleTrigger: async (_context, event: { id: string }) => { if (event.id === 'EvStuck') { isStuckStarted = true; await stuckReleased; } delivered.push(event.id); },
      }, { logger: newRecordingLogger() }) };
    };
    run(newMessageTriggerRunner({ connection, channelThreadCreatedRoutes: [route('stuck', 'stuck'), route('flood', 'flood')] }));
    socket.push(eventsEnvelope('envelope-EvStuck', 'EvStuck', rootMessage('1.0', { text: 'stuck' })));
    await expect.poll(() => isStuckStarted).toBe(true);
    const floodSize = 8;
    for (let index = 1; index <= floodSize; index++) socket.push(eventsEnvelope(`envelope-EvFlood${index}`, `EvFlood${index}`, rootMessage(`${index + 1}.0`, { text: 'flood' })));
    // Three queued envelopes, plus the one the reader holds while it waits for room.
    const held = capacity + 1;
    await expect.poll(() => socket.acknowledged.length).toBe(1 + held);
    await new Promise(resolve => setTimeout(resolve, 200));
    expect(socket.acknowledged).toEqual(['envelope-EvStuck', ...Array.from({ length: held }, (_value, index) => `envelope-EvFlood${index + 1}`)]);
    expect(reads).toBe(1 + held);
    releaseStuck();
    await expect.poll(() => delivered.length).toBe(1 + floodSize);
    expect(delivered).toEqual(['EvStuck', ...Array.from({ length: floodSize }, (_value, index) => `EvFlood${index + 1}`)]);
    expect(socket.acknowledged).toHaveLength(1 + floodSize);
  });

  it('consumes an undeliverable reply and keeps reading', async () => {
    const socket = new FakeSocket();
    const { connection, logger } = newSocketClient([socket]);
    const routes = new RecordingRoutes(event => event.id === 'EvClosed' ? markTriggerUndeliverable(new Error('Flow closed')) : undefined);
    run(newMessageTriggerRunner({ connection, threadReplyCreatedRoutes: [{ bindingName: 'reply', target: routes.target('reply'), configuration: { channelId: 'C1', threadReplyMatcher: { posterUserIds: ['U2'] } } }] }));
    socket.push(eventsEnvelope('e1', 'EvClosed', replyMessage('1.1', '1.0')));
    socket.push(eventsEnvelope('e2', 'EvOpen', replyMessage('2.1', '2.0')));
    await expect.poll(() => routes.deliveries).toEqual(['reply:EvOpen']);
    expect(logger.find('trigger event skipped: undeliverable', { event_id: 'EvClosed', binding: 'reply' })).toHaveLength(1);
  });

  it('names the cause of a rejected Socket Mode open without the token', async () => {
    const logger = newRecordingLogger();
    const sender = newSlackSender(() => ({ status: 200, body: { ok: false, error: 'invalid_auth' } }));
    const staticCredentials = { botToken: new SecretString('b'), userToken: new SecretString('u'), appToken: new SecretString('xapp-SENTINEL') };
    const client = new SlackClient({ endpoint: 'http://127.0.0.1:9/api' }, { resolve: async () => staticCredentials, resolveWithRefresh: async () => staticCredentials },
      { send: sender.send, logger, triggerTimings: timings, socketDialer: async () => { throw new Error('unused'); } });
    run(newMessageTriggerRunner({ connection: newConnection(client, { provider: 'slack', name: 'workspace' }), channelThreadCreatedRoutes: [{ bindingName: 'start', target: new RecordingRoutes().target('root'), configuration: { channelId: 'C1' } }] }));
    await expect.poll(() => logger.find('slack socket mode connection failed; reconnecting', { error: 'Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)' }).length).toBeGreaterThan(0);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });
});

describe('Slack relayed Triggers', () => {
  it('uses the unpadded base64url connection name as the path', () => {
    expect(relayedTriggerPath('support')).toBe('/triggers/slack/c3VwcG9ydA');
    expect(relayedTriggerPath('support-team/?')).toBe('/triggers/slack/c3VwcG9ydC10ZWFtLz8');
  });

  function newRelayRunner(host: RelayHost, routes: RecordingRoutes, logger = newRecordingLogger()) {
    const runner = newProjectMessageTriggerRunner(host.project, 'workspace', {
      channelThreadCreatedRoutes: [{ bindingName: 'start', target: routes.target('root') }],
      threadReplyCreatedRoutes: [{ bindingName: 'reply', target: routes.target('reply') }],
    }, { logger, triggerTimings: timings, send: newSlackSender(() => ({ status: 200, body: { ok: false, error: 'invalid_auth' } })).send });
    return { runner, logger };
  }

  it('registers, then records and delivers a root before its reply, choosing the mode from one credential read', async () => {
    const host = new RelayHost();
    const routes = new RecordingRoutes();
    const { runner, logger } = newRelayRunner(host, routes);
    const url = `${await serveRunner(runner, servers)}${relayedTriggerPath('workspace')}`;
    expect(await postRelayed(url, eventsPayload('EvEarly', rootMessage('1.0')))).toBe(503);
    const result = run(runner);
    await expect.poll(() => host.registrations.length).toBe(1);
    expect(host.registrations[0]).toEqual({ authorization: `Bearer ${relayScopeToken}`, body: { sharedConnectionId: 'conn-slack', connectionName: 'workspace' } });
    expect(logger.find('slack relayed trigger registered', { connection: 'workspace' })).toHaveLength(1);
    expect(await postRelayed(url, eventsPayload('EvRoot', rootMessage('1.0')))).toBe(200);
    expect(await postRelayed(url, eventsPayload('EvReply', replyMessage('1.1', '1.0')))).toBe(200);
    await expect.poll(() => routes.deliveries).toEqual(['root:EvRoot', 'reply:EvReply']);
    expect(routes.events.get('EvRoot')).toEqual({ id: 'EvRoot', occurredAt: new Date(1760227200_000),
      payload: { teamId: 'T1', channelId: 'C1', timestamp: '1.0', threadTimestamp: '1.0', userId: 'U1', text: `${sentinelText} request approval` } });
    await expect.poll(() => host.pendingEventIds()).toEqual([]);
    expect(host.credentialReads).toBe(1);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
    stops.at(-1)!.abort();
    expect(await result).toMatchObject({ name: 'TriggerDeliveryAbortedError' });
  });

  it('refuses unsigned, stale, wrongly signed, tampered, and oversized requests without reading a Slack credential', async () => {
    const host = new RelayHost();
    const routes = new RecordingRoutes();
    const { runner, logger } = newRelayRunner(host, routes);
    const url = `${await serveRunner(runner, servers)}${relayedTriggerPath('workspace')}`;
    run(runner);
    await expect.poll(() => host.registrations.length).toBe(1);
    const body = eventsPayload('EvRoot', rootMessage('1.0'));
    expect(await postRelayed(url, body, new Date(), 'another-token')).toBe(400);
    expect(await postRelayed(url, body, new Date(Date.now() - 6 * 60_000))).toBe(400);
    expect((await fetch(url, { method: 'POST', body: JSON.stringify(body) })).status).toBe(400);
    const signedAt = new Date();
    const { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest } = await import('@superdurable/connectors-sdk/relaysignature');
    const tampered = await fetch(url, { method: 'POST', body: JSON.stringify(eventsPayload('EvForged', rootMessage('1.0'))), headers: {
      [RelayTimestampHeader]: String(Math.floor(signedAt.getTime() / 1000)), [RelaySignatureHeader]: signRelayedRequest(relayScopeToken, signedAt, Buffer.from(JSON.stringify(body))) } });
    // The first mismatch already reread the scope token, so this one is retryable until the token can be reread.
    expect(tampered.status).toBe(503);
    expect(await postRelayed(url, `{"event_id":"EvLarge","pad":"${'x'.repeat(1 << 20)}"}`)).toBe(413);
    expect(await postRelayed(url, eventsPayload('EvOtherChannel', rootMessage('2.0', { channel: 'C9' })))).toBe(200);
    expect(await host.pendingEventIds()).toEqual([]);
    expect(routes.deliveries).toEqual([]);
    const refusals = logger.find('slack relayed request refused', { reason: 'invalid_signature' });
    expect(refusals).toHaveLength(3);
    expect(refusals[0]!.level).toBe('debug');
    expect(logger.find('slack relayed request refused', { reason: 'token_renewal_deferred' }).map(record => record.level)).toEqual(['debug']);
    expect(host.credentialReads).toBe(1);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });

  it('retries an unavailable host and stops on a refusal', async () => {
    const host = new RelayHost(attempt => attempt < 3 ? { status: 503 } : { status: 200, body: { status: 'REGISTERED' } });
    const { runner, logger } = newRelayRunner(host, new RecordingRoutes());
    run(runner);
    await expect.poll(() => host.registrations.length).toBe(3);
    expect(logger.find('slack relayed trigger registration failed; retrying')).toHaveLength(2);
    for (const answer of [{ status: 200, body: { status: 'NOT_RELAYED' } }, { status: 200, body: { status: 'CONNECTION_NOT_SELECTED' } }, { status: 409 }]) {
      const refused = new RelayHost(() => answer);
      const refusedRunner = newRelayRunner(refused, new RecordingRoutes()).runner;
      const error = await refusedRunner.run(newTriggerContext(AbortSignal.timeout(5_000))).catch((failure: unknown) => failure);
      expect(error).toBeInstanceOf(Error);
      expect((error as Error).name).not.toBe('TriggerDeliveryAbortedError');
      expect(String((error as Error).message)).not.toContain(relayScopeToken);
      const url = `${await serveRunner(refusedRunner, servers)}${relayedTriggerPath('workspace')}`;
      expect(await postRelayed(url, eventsPayload('EvRoot', rootMessage('1.0')))).toBe(503);
    }
  });

  it('uses Socket Mode when the shared credential has an app_token and stops when the connection is not selected', async () => {
    const host = new RelayHost();
    host.credential = { ...host.credential, app_token: 'xapp-SENTINEL-APP' };
    const { runner, logger } = newRelayRunner(host, new RecordingRoutes());
    run(runner);
    await expect.poll(() => logger.find('slack socket mode connection failed; reconnecting', { error: 'Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)' }).length).toBeGreaterThan(0);
    expect(host.registrations).toHaveLength(0);
    const url = `${await serveRunner(runner, servers)}${relayedTriggerPath('workspace')}`;
    expect(await postRelayed(url, eventsPayload('EvRoot', rootMessage('1.0')))).toBe(503);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);

    const unselected = new RelayHost();
    unselected.credentialStatus = 'CONNECTION_NOT_SELECTED';
    const error = await newRelayRunner(unselected, new RecordingRoutes()).runner.run(newTriggerContext(AbortSignal.timeout(5_000))).catch((failure: unknown) => failure);
    expect((error as Error).cause).toBeInstanceOf(ConnectionNotSelectedError);
    expect(unselected.registrations).toHaveLength(0);
  });

  it('answers 404 for a runner built without a project configuration', async () => {
    const { connection } = newSocketClient([]);
    const runner = newMessageTriggerRunner({ connection, channelThreadCreatedRoutes: [{ bindingName: 'start', target: new RecordingRoutes().target('root'), configuration: { channelId: 'C1' } }] });
    expect(await postRelayed(`${await serveRunner(runner, servers)}${relayedTriggerPath('workspace')}`, {})).toBe(404);
  });
});
