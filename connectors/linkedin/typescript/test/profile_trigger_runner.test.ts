// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// profileChanged runners, ported from the Go connector's profile_trigger_runner tests.
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import {
  decodeProfileChangedEventPayload, newConnectionWithOperations, newProfileChangedTrigger, newProfileTriggerRunner, newProjectConnection, newProjectProfileTriggerRunner,
  type ProfileChangedEvent,
} from '../src/index.js';
import { LinkedInProfileFake, adaProfile, newFakeConnection, newTestProject } from './linkedin_profile_fake.js';

const connectionName = 'linkedin-watcher';
const key = { connectorId: 'linkedin', connectionName, triggerName: 'profileChanged', bindingName: 'ownProfile' };

/** Consumes each event into a queue the test awaits. */
class QueueTarget implements TriggerTarget<ProfileChangedEvent> {
  readonly events: Array<TriggerEvent<ProfileChangedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<ProfileChangedEvent>): Promise<void> { this.events.push(event); }
  async nextEvent(index: number): Promise<TriggerEvent<ProfileChangedEvent>> {
    await eventually(() => this.events.length > index);
    return this.events[index]!;
  }
}

async function eventually(condition: () => Promise<boolean> | boolean): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

function running(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

describe('profileChanged runners', () => {
  it('rejects incomplete routes and a connection without a LinkedInClient', () => {
    const connection = newFakeConnection(new LinkedInProfileFake(adaProfile())), target = new QueueTarget();
    expect(() => newProfileTriggerRunner(connection, [])).toThrow();
    expect(() => newProfileTriggerRunner(connection, [{ bindingName: '', target }])).toThrow();
    expect(() => newProfileTriggerRunner(connection, [{ bindingName: 'ownProfile', target: undefined as never }])).toThrow();
    expect(() => newProfileTriggerRunner(connection, [{ bindingName: 'ownProfile', target, configuration: { member: 'other' } as never }])).toThrow('configuration');
    expect(() => newProfileTriggerRunner(connection, [{ bindingName: 'a', target }, { bindingName: 'a', target }])).toThrow();
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], { provider: 'linkedin', name: connectionName });
    expect(() => newProfileTriggerRunner(mock, [{ bindingName: 'ownProfile', target }])).toThrow('LinkedInClient');
  });

  it('records each change in the inbox before the cursor commits, and drains it', async () => {
    const fake = new LinkedInProfileFake(adaProfile());
    const { project } = await newTestProject(connectionName, []);
    const connection = newProjectConnection(project, connectionName, { send: fake.send });
    const target = new QueueTarget();
    const runner = newProjectProfileTriggerRunner(project, connection, [{ bindingName: 'ownProfile', target }]);
    const cursor = project.pollCursorStore(key);
    const commitCount = async () => (await cursor.readPollCursor()).state?.commitCount ?? 0;
    const process = running(runner);
    // The first poll otherwise waits a random jitter of up to 6 seconds, past Vitest's 5-second timeout.
    runner.wakePoller('ownProfile');
    try {
      await eventually(async () => await commitCount() >= 1);
      expect(target.events, 'the baseline starts nothing').toEqual([]);
      fake.update(profile => { profile.email = 'ada.king@example.com'; });
      runner.wakePoller('ownProfile');
      const event = await target.nextEvent(0);
      expect(event.payload.changedFields).toEqual(['email']);
      expect(event.payload.current.email).toBe('ada.king@example.com');
      await eventually(async () => await commitCount() >= 2);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token)).toMatchObject({ version: 1, subject: '782bbtaQ', sequence: 1 });
      const inbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('rejects an invalid saved configuration', async () => {
    const { project } = await newTestProject(connectionName, [{ bindingName: 'ownProfile', configuration: { member: 'other' } }]);
    const connection = newProjectConnection(project, connectionName, { send: new LinkedInProfileFake(adaProfile()).send });
    expect(() => newProjectProfileTriggerRunner(project, connection, [{ bindingName: 'ownProfile', target: new QueueTarget() }])).toThrow('ownProfile');
  });

  it('runs a single in-memory Trigger that baselines and delivers the next change', async () => {
    const fake = new LinkedInProfileFake(adaProfile());
    // A short interval keeps the test fast; LinkedInClient rejects one under a minute.
    const connection = newFakeConnection(fake);
    Object.defineProperty(connection.operations, 'pollIntervalMs', { value: 1_000 });
    const target = new QueueTarget();
    const process = running(newProfileChangedTrigger({ connection, bindingName: 'ownProfile', target }));
    try {
      await eventually(() => fake.requestCount() >= 1);
      fake.update(profile => { profile.given_name = 'Augusta'; });
      const event = await target.nextEvent(0);
      expect(event.payload.changedFields).toEqual(['givenName']);
      expect(event.payload.previous.givenName).toBe('Ada');
    } finally { await process.stop(); }
  });

  it('decodes a Go-written inbox payload', () => {
    const payload = '{"subject":"782bbtaQ","previous":{"subject":"782bbtaQ","name":"Ada \\u003cLovelace\\u003e","emailVerified":true},"current":{"subject":"782bbtaQ","name":"Ada King","emailVerified":false},"changedFields":["name","emailVerified"]}';
    const event = decodeProfileChangedEventPayload(payload);
    expect(event.previous.name).toBe('Ada <Lovelace>');
    expect(event.changedFields).toEqual(['name', 'emailVerified']);
    expect(() => decodeProfileChangedEventPayload('{"subject":"a","previous":{"subject":"a","emailVerified":false},"current":{"subject":"a","emailVerified":false},"changedFields":["headline"]}')).toThrow();
  });
});
