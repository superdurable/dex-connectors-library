// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, TriggerInbox } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, describe, expect, it } from 'vitest';
import { BlockActionsPayloadSchema, matchButtonClickPayload } from '../src/button_trigger.js';
import {
  ButtonClickedTriggerConfigurationSchema, defineButtonClickedTriggerBinding, newButtonClickedTrigger, newMessageTriggerRunner, newProjectMessageTriggerRunner,
  type ButtonClickEvent, type MessageEvent,
} from '../src/index.js';
import { buttonClickPayload, firstAction, interactiveEnvelope, newButtonSocketClient, newMemoryDurableTarget, sentinelButtonValue } from './button_support.js';
import { FakeSocket, RelayHost, eventsEnvelope, newRecordingLogger, rootMessage } from './trigger_support.js';

const stops: AbortController[] = [];
afterEach(() => { for (const stop of stops.splice(0)) stop.abort(); });

function run(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  stops.push(stop);
  return runner.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
}

describe('Slack buttonClicked decoding', () => {
  const configuration = { channelId: 'C1', actionIds: ['approve', 'reject'], clickerUserIds: ['U2'] };

  it('decodes a block_actions payload into a Go-identical event without interaction capabilities', () => {
    const threaded = BlockActionsPayloadSchema.parse(buttonClickPayload('C1', '1710000012.000700', '1710000000.000100', 'U2', 'approve', sentinelButtonValue, '1710000020.123456'));
    expect(matchButtonClickPayload(configuration, threaded)).toEqual({ ignored: [], events: [{
      id: 'T1:C1:1710000012.000700:approve:1710000020.123456', occurredAt: new Date(1710000020_123),
      payload: { teamId: 'T1', channelId: 'C1', messageTimestamp: '1710000012.000700', threadTimestamp: '1710000000.000100', userId: 'U2', actionId: 'approve', value: sentinelButtonValue, actionTimestamp: '1710000020.123456' },
    }] });
    // A top-level message reports itself as the thread root, as MessageEvent does.
    const topLevel = buttonClickPayload('C1', '1710000012.000700', '', 'U2', 'reject', '', '1710000021.000001');
    delete (topLevel.container as Record<string, unknown>).channel_id;
    const { events, ignored } = matchButtonClickPayload(configuration, BlockActionsPayloadSchema.parse(topLevel));
    expect(ignored).toEqual([]);
    expect(events).toHaveLength(1);
    expect(events[0]!.payload).toEqual({ teamId: 'T1', channelId: 'C1', messageTimestamp: '1710000012.000700', threadTimestamp: '1710000012.000700', userId: 'U2', actionId: 'reject', actionTimestamp: '1710000021.000001' });
    expect(JSON.stringify(events)).not.toMatch(/SENTINEL|hooks\.slack\.com|trigger_id|token/);
  });

  it('ignores unmatched clicks with the Go reasons and logs them at DEBUG without the value', async () => {
    const socket = new FakeSocket();
    const { connection, logger } = newButtonSocketClient([socket]);
    const delivered: string[] = [];
    run(newMessageTriggerRunner({ connection, buttonClickedRoutes: [{ bindingName: 'approval-decision', configuration: { channelId: 'C1', actionIds: ['approve'], clickerUserIds: ['U2'] },
      target: newMemoryDurableTarget<ButtonClickEvent>(new InMemoryObjectStore(), 'buttonClicked', 'approval-decision', async event => { delivered.push(event.id); }) }] }));
    const click = (change: (payload: Record<string, unknown>) => void) => { const payload = buttonClickPayload('C1', '5.0', '', 'U2', 'approve', sentinelButtonValue, '6.0'); change(payload); return payload; };
    const cases: Array<[string, Record<string, unknown>]> = [
      ['not_block_actions', click(payload => { payload.type = 'view_submission'; })],
      ['not_a_message', click(payload => { payload.container = { type: 'view', view_id: 'V1' }; })],
      ['channel_mismatch', click(payload => { (payload.container as Record<string, unknown>).channel_id = 'C9'; })],
      ['missing_user', click(payload => { delete payload.user; })],
      ['missing_message_identity', click(payload => { delete payload.team; })],
      ['clicker_mismatch', click(payload => { payload.user = { id: 'U3' }; })],
      ['not_a_button', click(payload => { firstAction(payload).type = 'static_select'; })],
      ['missing_action_identity', click(payload => { delete firstAction(payload).action_ts; })],
      ['action_mismatch', click(payload => { firstAction(payload).action_id = 'reject'; })],
      ['no_actions', click(payload => { payload.actions = []; })],
      // Go decodes a null action or payload as its zero value instead of failing.
      ['not_a_button', click(payload => { payload.actions = [null]; })],
    ];
    cases.push(['not_block_actions', null as unknown as Record<string, unknown>]);
    const envelopeIds = cases.map(([reason], index) => `env-${index}-${reason}`);
    cases.forEach(([, payload], index) => socket.push(interactiveEnvelope(envelopeIds[index]!, payload)));
    socket.push(interactiveEnvelope('env-bad', []));
    socket.push(interactiveEnvelope('env-match', click(() => undefined)));
    await expect.poll(() => delivered).toEqual(['T1:C1:5.0:approve:6.0']);
    expect(socket.acknowledged).toEqual([...envelopeIds, 'env-bad', 'env-match']);
    const ignored = logger.find('trigger event ignored');
    expect(ignored.map(record => [record.level, record.attributes.reason, record.attributes.envelope_id, record.attributes.binding, record.attributes.trigger]))
      .toEqual(cases.map(([reason], index) => ['debug', reason, envelopeIds[index], 'approval-decision', 'buttonClicked']));
    expect(logger.find('trigger event skipped: undecodable', { envelope_id: 'env-bad' })).toHaveLength(1);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });

  it('validates configurations as Go does', () => {
    expect(ButtonClickedTriggerConfigurationSchema.safeParse({ channelId: 'C1' }).success).toBe(true);
    const message = (value: unknown) => ButtonClickedTriggerConfigurationSchema.safeParse(value).error?.issues.map(issue => issue.message);
    expect(message({ channelId: '' })).toEqual(['Slack channel ID is required']);
    expect(message({ channelId: 'C1', actionIds: ['approve', 'approve'] })).toEqual(['Slack button action IDs must be non-empty and unique']);
    expect(message({ channelId: 'C1', clickerUserIds: [' '] })).toEqual(['Slack clicker user IDs must be non-empty and unique']);
    expect(message({ channelId: 'C1', unknown: true })).toBeDefined();
    expect(defineButtonClickedTriggerBinding({ connectionName: 'workspace', bindingName: 'decision', configurationUI: { units: [] } }).definition.trigger.triggerName).toBe('buttonClicked');
  });
});

describe('Slack buttonClicked runner', () => {
  it('acknowledges a click while an earlier root delivery retries, then delivers in arrival order on one connection', async () => {
    const socket = new FakeSocket();
    const { connection, dialed } = newButtonSocketClient([socket]);
    const objects = new InMemoryObjectStore();
    const order: string[] = [];
    let isRootBlocked = true;
    run(newMessageTriggerRunner({ connection,
      channelThreadCreatedRoutes: [{ bindingName: 'approval-start', configuration: { channelId: 'C1' }, target: newMemoryDurableTarget<MessageEvent>(objects, 'channelThreadCreated', 'approval-start', async event => {
        if (isRootBlocked) throw new Error('Worker stopped');
        order.push(`root:${event.id}`);
      }) }],
      buttonClickedRoutes: [{ bindingName: 'approval-decision', configuration: { channelId: 'C1', actionIds: ['approve'] }, target: newMemoryDurableTarget<ButtonClickEvent>(objects, 'buttonClicked', 'approval-decision', async event => {
        order.push(`click:${event.payload.actionId}`);
      }) }],
    }));
    socket.push(eventsEnvelope('envelope-EvRoot', 'EvRoot', rootMessage('1.0')));
    await expect.poll(() => socket.acknowledged).toEqual(['envelope-EvRoot']);
    const sentAt = Date.now();
    socket.push(interactiveEnvelope('env-click', buttonClickPayload('C1', '2.0', '1.0', 'U2', 'approve', 'order-1042', '3.0')));
    await expect.poll(() => socket.acknowledged, { interval: 5 }).toEqual(['envelope-EvRoot', 'env-click']);
    expect(Date.now() - sentAt).toBeLessThan(1_000);
    expect(order).toEqual([]);
    isRootBlocked = false;
    await expect.poll(() => order, { timeout: 5_000 }).toEqual(['root:EvRoot', 'click:approve']);
    expect(dialed).toHaveLength(1);
  });

  it('refuses button routes without durable inboxes, invalid routes, and the standalone factory', async () => {
    const { connection, dialed } = newButtonSocketClient([]);
    const plainClick = { handleTrigger: async () => undefined };
    const durableClick = newMemoryDurableTarget<ButtonClickEvent>(new InMemoryObjectStore(), 'buttonClicked', 'decision', async () => undefined);
    expect(() => newMessageTriggerRunner({ connection, buttonClickedRoutes: [{ bindingName: 'decision', configuration: { channelId: 'C1' }, target: plainClick }] })).toThrow(/durable inboxes/);
    expect(() => newMessageTriggerRunner({ connection,
      channelThreadCreatedRoutes: [{ bindingName: 'start', configuration: { channelId: 'C1' }, target: { handleTrigger: async () => undefined } }],
      buttonClickedRoutes: [{ bindingName: 'decision', configuration: { channelId: 'C1' }, target: durableClick }] })).toThrow(/durable inboxes/);
    expect(() => newMessageTriggerRunner({ connection, buttonClickedRoutes: [{ bindingName: 'decision', configuration: { channelId: '' }, target: durableClick }] })).toThrow(/channel ID is required/);
    expect(() => newMessageTriggerRunner({ connection, buttonClickedRoutes: [
      { bindingName: 'decision', configuration: { channelId: 'C1' }, target: durableClick }, { bindingName: 'decision', configuration: { channelId: 'C1' }, target: durableClick },
    ] })).toThrow(/duplicated/);
    for (const target of [plainClick, durableClick]) {
      const trigger = newButtonClickedTrigger({ connection, connectionName: 'workspace', bindingName: 'decision', configuration: { channelId: 'C1' }, target });
      expect(await run(trigger)).toMatchObject({ message: expect.stringMatching(/durable inboxes/) });
    }
    expect(dialed).toEqual([]);
  });

  it('records a click in the project inbox before acknowledging, and replays it after a restart', async () => {
    const host = new RelayHost(undefined, { connectionName: 'workspace', startBinding: 'start', replyBinding: 'reply', slackEndpoint: 'http://127.0.0.1:9/api', triggerBindings: [
      { connectorId: 'slack', connectionName: 'workspace', triggerName: 'buttonClicked', bindingName: 'decision', configuration: { channelId: 'C1', clickerUserIds: ['U2'] } },
    ] });
    host.credential = { ...host.credential, app_token: 'xapp-SENTINEL-APP' };
    const [first, second] = [new FakeSocket(), new FakeSocket()];
    const sockets = [first, second];
    const logger = newRecordingLogger();
    let isBlocked = true;
    const delivered: TriggerEvent<ButtonClickEvent>[] = [];
    const newRunner = () => newProjectMessageTriggerRunner(host.project, 'workspace', { buttonClickedRoutes: [{ bindingName: 'decision', target: { handleTrigger: async (_context, event) => {
      if (isBlocked) throw new Error('Worker stopped');
      delivered.push(event);
    } } }] }, { logger, socketDialer: async () => sockets.shift()!, triggerTimings: { reconnectDelayMs: 2, reconnectMaxDelayMs: 8, readTimeoutMs: 2_000 },
      send: async () => ({ status: 200, headers: {}, body: Buffer.from(JSON.stringify({ ok: true, url: 'wss://wss-primary.slack.com/link/?ticket=SENTINEL-TICKET' })) }) });
    const firstStop = new AbortController();
    const firstRun = newRunner().run(newTriggerContext(firstStop.signal)).catch((error: unknown) => error);
    first.push(interactiveEnvelope('env-click', buttonClickPayload('C1', '2.0', '1.0', 'U2', 'approve', 'order-1042', '3.0')));
    await expect.poll(() => first.acknowledged).toEqual(['env-click']);
    const pending = () => new TriggerInbox(host.objects, { projectId: 'relay', kind: 'live' }, { connectorId: 'slack', connectionName: 'workspace', triggerName: 'buttonClicked', bindingName: 'decision' }).pending();
    expect((await pending()).map(event => event.id)).toEqual(['T1:C1:2.0:approve:3.0']);
    firstStop.abort();
    await firstRun;
    isBlocked = false;
    run(newRunner());
    await expect.poll(() => delivered.map(event => event.id)).toEqual(['T1:C1:2.0:approve:3.0']);
    expect(delivered[0]!.payload).toEqual({ teamId: 'T1', channelId: 'C1', messageTimestamp: '2.0', threadTimestamp: '1.0', userId: 'U2', actionId: 'approve', value: 'order-1042', actionTimestamp: '3.0' });
    await expect.poll(pending).toEqual([]);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });

  it('refuses button routes on a relayed connection without an app_token', async () => {
    const host = new RelayHost(undefined, { connectionName: 'workspace', startBinding: 'start', replyBinding: 'reply', slackEndpoint: 'http://127.0.0.1:9/api', triggerBindings: [
      { connectorId: 'slack', connectionName: 'workspace', triggerName: 'buttonClicked', bindingName: 'decision', configuration: { channelId: 'C1' } },
    ] });
    const runner = newProjectMessageTriggerRunner(host.project, 'workspace', { buttonClickedRoutes: [{ bindingName: 'decision', target: { handleTrigger: async () => undefined } }] }, { logger: newRecordingLogger() });
    expect(await run(runner)).toMatchObject({ message: 'Slack connection "workspace" has no app_token for Socket Mode, and button clicks reach only Socket Mode; add an app_token or remove the buttonClicked routes' });
    expect(host.registrations).toEqual([]);
  });
});
