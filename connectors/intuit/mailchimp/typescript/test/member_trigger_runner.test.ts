// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// memberChanged runners, ported from the Go connector's member_trigger_runner tests.
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { MailchimpClient } from '../src/client.js';
import {
  decodeMemberChangedEventPayload, newConnection, newConnectionWithOperations, newMemberChangedTrigger, newMemberTriggerRunner, newProjectConnection, newProjectMemberTriggerRunner,
  type MemberChangedEvent,
} from '../src/index.js';
import { APIBaseURL, MailchimpAudienceFake, audienceId, newFakeClient, newTestProject } from './mailchimp_audience_fake.js';

const connectionName = 'mailchimp-fetcher-test';
const reference = { provider: 'mailchimp', name: connectionName };
const key = { connectorId: 'mailchimp', connectionName, triggerName: 'memberChanged', bindingName: 'audienceChanges' };
const audience = { audienceId };

/** Consumes each event into a list the test awaits. */
class QueueTarget implements TriggerTarget<MemberChangedEvent> {
  readonly events: Array<TriggerEvent<MemberChangedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<MemberChangedEvent>): Promise<void> { this.events.push(event); }
  async nextEvent(index: number): Promise<TriggerEvent<MemberChangedEvent>> {
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

describe('memberChanged runners', () => {
  it('rejects incomplete routes and a connection without a MailchimpClient', () => {
    const connection = newConnection(newFakeClient(new MailchimpAudienceFake(audienceId)), reference), target = new QueueTarget();
    expect(() => newMemberTriggerRunner(connection, [])).toThrow();
    expect(() => newMemberTriggerRunner(connection, [{ bindingName: '', configuration: audience, target }])).toThrow();
    expect(() => newMemberTriggerRunner(connection, [{ bindingName: 'audienceChanges', configuration: audience, target: undefined as never }])).toThrow();
    expect(() => newMemberTriggerRunner(connection, [{ bindingName: 'a', configuration: audience, target }, { bindingName: 'a', configuration: audience, target }])).toThrow();
    expect(() => newMemberTriggerRunner(connection, [{ bindingName: 'audienceChanges', configuration: { audienceId: '' }, target }])).toThrow('audienceId');
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], reference);
    expect(() => newMemberTriggerRunner(mock, [{ bindingName: 'audienceChanges', configuration: audience, target }])).toThrow('MailchimpClient');
  });

  it('requires a saved, valid audience', async () => {
    const fake = new MailchimpAudienceFake(audienceId);
    const { project } = await newTestProject(connectionName, []);
    const connection = newProjectConnection(project, connectionName, { apiBaseURL: APIBaseURL, send: fake.send });
    expect(() => newProjectMemberTriggerRunner(project, connection, [{ bindingName: 'audienceChanges', target: new QueueTarget() }])).toThrow('no saved audience');
    const invalid = (await newTestProject(connectionName, [{ bindingName: 'audienceChanges', configuration: { audienceId, statuses: ['gone'] } }])).project;
    expect(() => newProjectMemberTriggerRunner(invalid, connection, [{ bindingName: 'audienceChanges', target: new QueueTarget() }])).toThrow('not a Mailchimp contact status');
  });

  it('loads the saved audience and statuses and commits after recording', async () => {
    const fake = new MailchimpAudienceFake(audienceId);
    fake.upsert('ben@example.com', 'subscribed', 'Ben');
    const { project } = await newTestProject(connectionName, [{ bindingName: 'audienceChanges', configuration: { audienceId, statuses: ['subscribed'] } }]);
    const connection = newProjectConnection(project, connectionName, { apiBaseURL: APIBaseURL, send: fake.send });
    expect((connection.operations as MailchimpClient).pollIntervalMs, 'the stored nanoseconds become milliseconds').toBe(5000);
    const target = new QueueTarget();
    const runner = newProjectMemberTriggerRunner(project, connection, [{ bindingName: 'audienceChanges', target }]);
    const cursor = project.pollCursorStore(key);
    const process = running(runner);
    try {
      await eventually(async () => ((await cursor.readPollCursor()).state?.commitCount ?? 0) >= 1);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token).baseline).toBe('2026-11-01T09:00:01Z');
      fake.upsert('cara@example.com', 'unsubscribed', 'Cara');
      fake.upsert('dana@example.com', 'subscribed', 'Dana');
      runner.wakePoller('audienceChanges');
      const event = await target.nextEvent(0);
      expect(event.payload.member.emailAddress, 'the saved statuses filter').toBe('dana@example.com');
      expect(event.payload.audienceId).toBe(audienceId);
      const inbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('baselines and delivers the next change with the single in-memory Trigger', async () => {
    const fake = new MailchimpAudienceFake(audienceId);
    fake.upsert('ben@example.com', 'subscribed', 'Ben');
    const target = new QueueTarget();
    const trigger = newMemberChangedTrigger({ connection: newConnection(newFakeClient(fake, 5000), reference), bindingName: 'audienceChanges', configuration: audience, target });
    const process = running(trigger);
    try {
      await eventually(() => fake.requests.length >= 1);
      fake.upsert('ben@example.com', 'unsubscribed', 'Ben');
      const event = await target.nextEvent(0);
      expect(event.payload.member.status).toBe('unsubscribed');
      expect(event.occurredAt.toISOString()).toBe('2026-11-01T09:00:02.000Z');
    } finally { await process.stop(); }
  }, 20_000);

  it('decodes a Go-written inbox payload', () => {
    const goPayload = '{"audienceId":"57afe96172","member":{"subscriberHash":"2c66f6142933ed5a97948f89cb1c7be0","emailAddress":"ben@example.com",'
      + '"status":"unsubscribed","mergeFields":{"FNAME":"Ben \\u003cB\\u003e"},"tagCount":0,"listId":"57afe96172","lastChangedAt":"2026-11-01T09:00:06Z"}}';
    expect(decodeMemberChangedEventPayload(goPayload).member.mergeFields).toEqual({ FNAME: 'Ben <B>' });
    expect(() => decodeMemberChangedEventPayload('{"member":{"status":"subscribed"}}')).toThrow();
  });
});
