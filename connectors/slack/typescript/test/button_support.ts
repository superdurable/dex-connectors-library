// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget } from '@superdurable/connectors-sdk/projectconfig';
import { SlackClient } from '../src/client.js';
import { newConnection, type SlackSocketDialer } from '../src/index.js';
import { newRecordingLogger, newSlackSender, sentinelText, type FakeSocket } from './trigger_support.js';

/** Interaction capabilities that must never reach an event or record. */
export const sentinelButtonValue = 'SENTINEL-BUTTON-VALUE';
const sentinelVerifyToken = 'SENTINEL-VERIFY-TOKEN';
const sentinelTriggerID = 'SENTINEL-TRIGGER-ID';
const sentinelResponseURL = 'https://hooks.slack.com/actions/SENTINEL-RESPONSE';

/** A block_actions payload with one button action, shaped as Slack sends it and as the Go tests build it. */
export function buttonClickPayload(channelId: string, messageTimestamp: string, threadTimestamp: string, userId: string, actionId: string, value: string, actionTimestamp: string): Record<string, unknown> {
  const container: Record<string, unknown> = { type: 'message', message_ts: messageTimestamp, channel_id: channelId, is_ephemeral: false };
  const message: Record<string, unknown> = { type: 'message', ts: messageTimestamp, user: 'UBOT', bot_id: 'B1', text: sentinelText };
  if (threadTimestamp) { container.thread_ts = threadTimestamp; message.thread_ts = threadTimestamp; }
  const action: Record<string, unknown> = { type: 'button', action_id: actionId, block_id: 'decision', action_ts: actionTimestamp, text: { type: 'plain_text', text: 'Approve', emoji: true } };
  if (value) action.value = value;
  return {
    type: 'block_actions', token: sentinelVerifyToken, trigger_id: sentinelTriggerID, response_url: sentinelResponseURL, api_app_id: 'A1',
    team: { id: 'T1', domain: 'acme' }, user: { id: userId, username: 'approver', team_id: 'T1' }, channel: { id: channelId, name: 'approvals' },
    container, message, actions: [action],
  };
}

/** The first action of a payload. */
export function firstAction(payload: Record<string, unknown>): Record<string, unknown> {
  return (payload.actions as Record<string, unknown>[])[0]!;
}

/** Socket Mode envelope of an interactive payload. */
export function interactiveEnvelope(envelopeId: string, payload: unknown): Record<string, unknown> {
  return { envelope_id: envelopeId, type: 'interactive', payload };
}

/** A Slack connection whose Socket Mode dials the given fake sockets. */
export function newButtonSocketClient(sockets: FakeSocket[], logger = newRecordingLogger(), appToken = 'xapp-SENTINEL-APP') {
  const dialed: string[] = [];
  const socketDialer: SlackSocketDialer = async url => {
    dialed.push(url);
    const socket = sockets.shift();
    if (!socket) throw new Error('no socket');
    return socket;
  };
  const credentials = { botToken: new SecretString('xoxb-SENTINEL-BOT'), userToken: new SecretString('xoxp-SENTINEL-USER'), appToken: new SecretString(appToken) };
  const client = new SlackClient({ endpoint: 'http://127.0.0.1:9/api' }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials },
    { send: newSlackSender().send, socketDialer, logger, triggerTimings: { reconnectDelayMs: 2, reconnectMaxDelayMs: 8, readTimeoutMs: 2_000 } });
  return { connection: newConnection(client, { provider: 'slack', name: 'workspace' }), logger, dialed };
}

/** A durable inbox target over an in-memory store, which a button route requires. */
export function newMemoryDurableTarget<Event>(objects: InMemoryObjectStore, triggerName: string, bindingName: string, handle: (event: TriggerEvent<Event>) => Promise<void>): TriggerTarget<Event> {
  const key = { connectorId: 'slack', connectionName: 'workspace', triggerName, bindingName };
  return newDurableTriggerTarget(new TriggerInbox(objects, { projectId: 'p', kind: 'live' }, key), key, { handleTrigger: async (_context, event) => handle(event) }, { logger: newRecordingLogger() });
}
