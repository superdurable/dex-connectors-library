// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  TwilioClient, newConnection, newMessageReceivedTrigger, newMessageTriggerRunner, newProjectConnection, newProjectMessageTriggerRunner,
  type Connection, type MessageReceivedEvent,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { TwilioMessagesFake } from './twilio_messages_fake.js';

const accountSid = 'AC' + '00112233445566778899aabbccddeeff';
const authToken = 'fetcher-auth-token-0123456789abcd';
const number = '+14155550100';
const otherNumber = '+14155550199';
const customer = '+14155550123';
const reference = { provider: 'twilio', name: 'twilio-messaging' };

function newTriggerConnection(fake: TwilioMessagesFake): Connection {
  const client = new TwilioClient({ accountSid, endpoint: 'http://127.0.0.1:24210/2010-04-01', pollIntervalMs: 5_000 },
    new StaticCredentialProvider([[reference, { authMethodId: 'auth-token', authToken: new SecretString(authToken) }]]), { send: fake.send });
  return newConnection(client, reference.name);
}
const secondsAgo = (seconds: number) => new Date(Date.now() - seconds * 1000);

describe('messageReceived runners', () => {
  it('reads pollInterval from the saved connection as a Go duration', () => {
    const project = (configuration: Record<string, unknown>) => ({
      configuration: { connections: [{ connectorId: 'twilio-messaging', connectionName: 'c', provider: 'twilio', configuration: { accountSid, ...configuration } }] },
      connections: { scope: { projectId: 'p', kind: 'live' } },
    }) as unknown as LoadedProject;
    expect((newProjectConnection(project({ pollInterval: 15_000_000_000 }), 'c').operations as TwilioClient).pollIntervalMs).toBe(15_000);
    expect((newProjectConnection(project({}), 'c').operations as TwilioClient).pollIntervalMs).toBe(30_000);
    expect((newProjectConnection(project({ pollInterval: 0 }), 'c').operations as TwilioClient).pollIntervalMs).toBe(30_000);
    expect(() => newProjectConnection(project({ pollInterval: 1_000_000_000 }), 'c')).toThrow('at least 5s');
    // Go's json.Unmarshal refuses a text duration for time.Duration.
    expect(() => newProjectConnection(project({ pollInterval: '2m' }), 'c')).toThrow();
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newTriggerConnection(new TwilioMessagesFake(accountSid, accountSid, authToken));
    const target = { handleTrigger: async () => undefined };
    for (const routes of [[], [{ bindingName: '', configuration: {}, target }], [{ bindingName: 'inbound', configuration: {}, target: undefined as never }],
      [{ bindingName: 'inbound', configuration: {}, target }, { bindingName: 'inbound', configuration: {}, target }],
      [{ bindingName: 'inbound', configuration: { to: '4155550100' }, target }]]) {
      expect(() => newMessageTriggerRunner(connection, routes)).toThrow();
    }
    expect(() => newMessageTriggerRunner(new GeneratedMock('twilio-messaging').connection(), [{ bindingName: 'inbound', configuration: {}, target }])).toThrow('mock connection');
  });

  it('loads the saved address and commits after recording each message', async () => {
    const fake = new TwilioMessagesFake(accountSid, accountSid, authToken);
    fake.receive(customer, number, 'existing', secondsAgo(10));
    const project = new LoadedProject({
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'twilio-trigger-test', kind: 'live' }, revision: 1, operationConfigurations: [],
      connections: [{ connectorId: 'twilio-messaging', connectionName: 'twilio-messaging', modulePath: 'twilio-messaging', provider: 'twilio', configuration: { accountSid } }],
      triggerBindings: [{ connectorId: 'twilio-messaging', connectionName: 'twilio-messaging', triggerName: 'messageReceived', bindingName: 'support-line', configuration: { to: number } }],
    }, undefined, new InMemoryObjectStore(), undefined, undefined);
    const delivered: Array<TriggerEvent<MessageReceivedEvent>> = [];
    const runner = newProjectMessageTriggerRunner(project, newTriggerConnection(fake), [{ bindingName: 'support-line', target: { handleTrigger: async (_context, message) => { delivered.push(message); } } }]);
    const key = { connectorId: 'twilio-messaging', connectionName: 'twilio-messaging', triggerName: 'messageReceived', bindingName: 'support-line' };
    const committed = async () => (await project.pollCursorStore(key).readPollCursor()).state;
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal)).catch(error => error);
    try {
      await expect.poll(async () => (await committed())?.token ?? '', { timeout: 10_000 }).toContain(`"to":"${number}"`);
      const sid = fake.receive(customer, number, 'new', new Date());
      fake.receive(customer, otherNumber, 'elsewhere', new Date());
      runner.wakePoller('support-line');
      await expect.poll(() => delivered.length, { timeout: 10_000 }).toBe(1);
      expect([delivered[0]!.id, delivered[0]!.payload.body], 'the baseline message is never delivered').toEqual([`twilio-message-${sid}`, 'new']);
      await expect.poll(async () => {
        const state = await committed();
        return (state?.commitCount ?? 0) >= 2 && state?.snapshot?.entries.length === 2;
      }, { timeout: 10_000 }).toBe(true);
      await expect.poll(async () => (await project.triggerInbox(key).pending()).length, { timeout: 10_000 }).toBe(0);
      expect(delivered, 'a message to another number is not delivered').toHaveLength(1);
    } finally { stop.abort(); await running; }
  });

  it('runs one binding with a memory cursor, baselining and delivering the next message', async () => {
    const fake = new TwilioMessagesFake(accountSid, accountSid, authToken);
    fake.receive(customer, number, 'existing', secondsAgo(10));
    const delivered: Array<TriggerEvent<MessageReceivedEvent>> = [];
    const trigger = newMessageReceivedTrigger({
      connection: newTriggerConnection(fake), connectionName: 'twilio-messaging', bindingName: 'every-inbound', configuration: {},
      target: { handleTrigger: async (_context, message) => { delivered.push(message); } },
    });
    expect(() => newMessageReceivedTrigger({ connection: newTriggerConnection(fake), connectionName: 'other', bindingName: 'b', configuration: {}, target: { handleTrigger: async () => undefined } })).toThrow('must equal');
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch(error => error);
    try {
      await expect.poll(() => fake.listRequests.length, { timeout: 10_000 }).toBeGreaterThan(0);
      const sid = fake.receive(customer, otherNumber, 'hi', new Date());
      // The next poll comes one jittered 5s interval later.
      await expect.poll(() => delivered.length, { timeout: 20_000, interval: 100 }).toBe(1);
      expect([delivered[0]!.id, delivered[0]!.payload.message.to], 'a blank address receives every number').toEqual([`twilio-message-${sid}`, otherNumber]);
    } finally { stop.abort(); await running; }
  }, 30_000);
});
