// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// meetingChanged runners, ported from the Go connector's meeting_trigger_runner tests.
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { TriggerInbox } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  decodeMeetingChangedEventPayload, newConnectionWithOperations, newMeetingChangedTrigger, newMeetingTriggerRunner, newProjectConnection, newProjectMeetingTriggerRunner,
  type MeetingChangedEvent,
} from '../src/index.js';
import { ZoomMeetingsFake, newFakeConnection, newTestProject } from './zoom_meetings_fake.js';

const hostId = 'zoom-host-1';
const connectionName = 'zoom-watcher';
const key = { connectorId: 'zoom', connectionName, triggerName: 'meetingChanged', bindingName: 'hostMeetings' };

/** Consumes each event into a queue the test awaits. */
class QueueTarget implements TriggerTarget<MeetingChangedEvent> {
  readonly events: Array<TriggerEvent<MeetingChangedEvent>> = [];
  #waiters: Array<() => void> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<MeetingChangedEvent>): Promise<void> { this.events.push(event); for (const wake of this.#waiters.splice(0)) wake(); }
  async nextEvent(index: number): Promise<TriggerEvent<MeetingChangedEvent>> {
    const deadline = Date.now() + 10_000;
    while (this.events.length <= index) {
      if (Date.now() > deadline) throw new Error(`no event ${index}`);
      await new Promise<void>(resolve => { this.#waiters.push(resolve); setTimeout(resolve, 50); });
    }
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

describe('meetingChanged runners', () => {
  it('rejects incomplete routes and a connection without a ZoomClient', () => {
    const connection = newFakeConnection(new ZoomMeetingsFake(hostId)), target = new QueueTarget();
    expect(() => newMeetingTriggerRunner(connection, [])).toThrow();
    expect(() => newMeetingTriggerRunner(connection, [{ bindingName: '', target }])).toThrow();
    expect(() => newMeetingTriggerRunner(connection, [{ bindingName: 'meetingChanges', target: undefined as never }])).toThrow();
    expect(() => newMeetingTriggerRunner(connection, [{ bindingName: 'meetingChanges', target, configuration: { changeKinds: ['ended'] } }])).toThrow('must be scheduled');
    expect(() => newMeetingTriggerRunner(connection, [{ bindingName: 'a', target }, { bindingName: 'a', target }])).toThrow();
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], { provider: 'zoom', name: connectionName });
    expect(() => newMeetingTriggerRunner(mock, [{ bindingName: 'meetingChanges', target }])).toThrow('ZoomClient');
  });

  it('loads the saved kinds, gives back-and-forth renames new IDs, and drains the inbox', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1);
    const { project } = await newTestProject(connectionName, [{ bindingName: 'hostMeetings', configuration: { changeKinds: ['scheduled', 'updated'] } }]);
    const connection = newProjectConnection(project, connectionName, { send: fake.send });
    const target = new QueueTarget();
    const runner = newProjectMeetingTriggerRunner(project, connection, [{ bindingName: 'hostMeetings', target }]);
    const cursor = project.pollCursorStore(key);
    const commitCount = async () => (await cursor.readPollCursor()).state?.commitCount ?? 0;
    const process = running(runner);
    try {
      await eventually(async () => await commitCount() >= 1);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token)).toEqual({ version: 1, hostId });
      const seen = new Set<string>();
      for (const [index, topic] of ['Consultation moved', 'Consultation', 'Consultation moved'].entries()) {
        fake.rename(92674392836, topic);
        runner.wakePoller('hostMeetings');
        const event = await target.nextEvent(index);
        expect(event.payload.changeKind).toBe('updated');
        expect(event.payload.meeting.topic).toBe(topic);
        expect(seen.has(event.id), `change ${index} reuses an earlier event ID`).toBe(false);
        seen.add(event.id);
        await eventually(async () => await commitCount() >= index + 2);
      }
      fake.delete(92674392836); fake.schedule(92674392850, 'Kickoff', 4);
      runner.wakePoller('hostMeetings');
      const event = await target.nextEvent(3);
      expect(event.payload.changeKind, 'the saved kinds skip the removal').toBe('scheduled');
      expect(event.payload.meeting.id).toBe(92674392850);
      const inbox: TriggerInbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('rejects an invalid saved configuration', async () => {
    const { project } = await newTestProject(connectionName, [{ bindingName: 'hostMeetings', configuration: { changeKinds: ['scheduled'], unknown: true } }]);
    const connection = newProjectConnection(project, connectionName, { send: new ZoomMeetingsFake(hostId).send });
    expect(() => newProjectMeetingTriggerRunner(project, connection, [{ bindingName: 'hostMeetings', target: new QueueTarget() }])).toThrow('hostMeetings');
  });

  it('runs a single in-memory Trigger that baselines and delivers the next change', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1);
    // A short interval keeps the test fast; ZoomClient rejects one under 30 seconds.
    const connection = newFakeConnection(fake);
    Object.defineProperty(connection.operations, 'pollIntervalMs', { value: 1_000 });
    const target = new QueueTarget();
    const process = running(newMeetingChangedTrigger({ connection, bindingName: 'hostMeetings', target }));
    try {
      await eventually(() => fake.listingRequestCount() >= 1);
      fake.rename(92674392836, 'Consultation moved');
      const event = await target.nextEvent(0);
      expect(event.payload.changeKind).toBe('updated');
      expect(event.payload.previousMeeting?.topic).toBe('Consultation');
    } finally { await process.stop(); }
  });

  it('decodes a Go-written inbox payload', () => {
    const payload = '{"changeKind":"updated","meeting":{"id":1001,"uuid":"uuid-1001==","hostId":"zoom-host-1","topic":"Consultation \\u003cmoved\\u003e","type":2,"startTime":"2036-02-24T03:30:00.12Z","joinUrl":"https://us05web.zoom.us/j/1001"},"previousMeeting":{"id":1001,"topic":"Consultation"}}';
    const event = decodeMeetingChangedEventPayload(payload);
    expect(event.meeting.topic).toBe('Consultation <moved>');
    expect(event.previousMeeting?.topic).toBe('Consultation');
    expect(() => decodeMeetingChangedEventPayload('{"changeKind":"ended","meeting":{"id":1,"topic":""}}')).toThrow();
  });
});
