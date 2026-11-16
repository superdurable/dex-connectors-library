// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// employeeChanged runners, ported from the Go connector's employee_changed_trigger_runner tests.
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import {
  BambooHRClient, EmployeeChangeSettleWindowMs, decodeEmployeeChangedEventPayload, newConnectionWithOperations, newEmployeeChangedTrigger, newEmployeeChangedTriggerRunner,
  newProjectConnection, newProjectEmployeeChangedTriggerRunner, type EmployeeChangedEvent,
} from '../src/index.js';
import { BambooHRFake, TriggerTestConnection, newFakeConnection, newTestProject } from './bamboohr_fake.js';

const connectionName = TriggerTestConnection.name;
const key = { connectorId: 'bamboohr', connectionName, triggerName: 'employeeChanged', bindingName: 'hires' };

/** Consumes each event into a list the test awaits. */
class QueueTarget implements TriggerTarget<EmployeeChangedEvent> {
  readonly events: Array<TriggerEvent<EmployeeChangedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<EmployeeChangedEvent>): Promise<void> { this.events.push(event); }
  async nextEvent(index: number): Promise<TriggerEvent<EmployeeChangedEvent>> {
    await eventually(() => this.events.length > index, 20_000);
    return this.events[index]!;
  }
}

async function eventually(condition: () => Promise<boolean> | boolean, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

function running(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

describe('employeeChanged runners', () => {
  it('rejects a short poll interval and keeps the manifest default', () => {
    const credentials = { resolve: async () => ({ apiKey: new SecretString('key') }) };
    expect(() => new BambooHRClient({ companyDomain: 'acme', pollIntervalMs: 9_000 }, credentials)).toThrow('pollInterval must be at least 10s');
    expect(new BambooHRClient({ companyDomain: 'acme' }, credentials).pollIntervalMs).toBe(60_000);
  });

  it('rejects incomplete routes and a connection without a BambooHRClient', () => {
    const connection = newFakeConnection(new BambooHRFake()), target = new QueueTarget();
    expect(() => newEmployeeChangedTriggerRunner(connection, [])).toThrow();
    expect(() => newEmployeeChangedTriggerRunner(connection, [{ bindingName: '', target }])).toThrow();
    expect(() => newEmployeeChangedTriggerRunner(connection, [{ bindingName: 'hires', target: undefined as never }])).toThrow();
    expect(() => newEmployeeChangedTriggerRunner(connection, [{ bindingName: 'hires', target }, { bindingName: 'hires', target }])).toThrow();
    expect(() => newEmployeeChangedTriggerRunner(connection, [{ bindingName: 'hires', target, configuration: { changeType: 'all' } }])).toThrow('changeType must be inserted');
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], TriggerTestConnection);
    expect(() => newEmployeeChangedTriggerRunner(mock, [{ bindingName: 'hires', target }])).toThrow('BambooHRClient');
    expect(() => newProjectEmployeeChangedTriggerRunner(undefined as never, connection, [])).toThrow('loaded project configuration');
  });

  it('loads the saved change type and commits after recording', async () => {
    const fake = new BambooHRFake();
    fake.insert('140');
    const { project } = await newTestProject(connectionName, [{ bindingName: 'hires', configuration: { changeType: 'inserted' } }]);
    const connection = newProjectConnection(project, connectionName, { send: fake.send });
    const target = new QueueTarget();
    const runner = newProjectEmployeeChangedTriggerRunner(project, connection, [{ bindingName: 'hires', target }]);
    const cursor = project.pollCursorStore(key);
    const committedToken = async () => (await cursor.readPollCursor()).state?.token ?? '';
    const process = running(runner);
    try {
      await eventually(async () => await committedToken() !== '');
      expect(await committedToken()).toBe('{"v":1,"since":"2026-10-13T09:00:01Z","changeType":"inserted"}');
      fake.update('140');
      fake.insert('141');
      fake.advance(EmployeeChangeSettleWindowMs);
      runner.wakePoller('hires');
      const event = await target.nextEvent(0);
      expect(event.payload.employeeId, 'the seeded and updated employees are not delivered').toBe('141');
      expect(event.payload.action).toBe('Inserted');
      await eventually(async () => await committedToken() === '{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"141","changeType":"inserted"}');
      const inbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('rejects an invalid saved configuration', async () => {
    for (const [configuration, message] of [[{ changeType: 'all' }, 'changeType must be inserted'], [{ changeType: 'inserted', unknown: true }, 'binding "hires" configuration']] as const) {
      const { project } = await newTestProject(connectionName, [{ bindingName: 'hires', configuration }]);
      const connection = newProjectConnection(project, connectionName, { send: new BambooHRFake().send });
      expect(() => newProjectEmployeeChangedTriggerRunner(project, connection, [{ bindingName: 'hires', target: new QueueTarget() }])).toThrow(message);
    }
  });

  it('delivers every change type for an unsaved binding', async () => {
    const fake = new BambooHRFake();
    const { project } = await newTestProject(connectionName, []);
    const connection = newProjectConnection(project, connectionName, { send: fake.send });
    const target = new QueueTarget();
    const runner = newProjectEmployeeChangedTriggerRunner(project, connection, [{ bindingName: 'hires', target }]);
    const process = running(runner);
    try {
      await eventually(() => fake.changeListQueries.length >= 1);
      fake.delete('77');
      fake.advance(EmployeeChangeSettleWindowMs);
      runner.wakePoller('hires');
      expect((await target.nextEvent(0)).payload.action).toBe('Deleted');
      expect(fake.changeListQueries.every(query => !query.has('type'))).toBe(true);
    } finally { await process.stop(); }
  });

  it('runs a single in-memory Trigger that seeds and delivers the next change', async () => {
    const fake = new BambooHRFake();
    fake.insert('140');
    const connection = newFakeConnection(fake), target = new QueueTarget();
    const process = running(newEmployeeChangedTrigger({ connection, target }));
    try {
      await eventually(() => fake.changeListQueries.length >= 1);
      fake.delete('140');
      fake.advance(EmployeeChangeSettleWindowMs);
      // The single Trigger has no wake hook; it polls again after the 10-second interval.
      expect((await target.nextEvent(0)).payload).toEqual({ employeeId: '140', action: 'Deleted', lastChanged: '2026-10-13T09:00:02Z' });
    } finally { await process.stop(); }
    expect(() => newEmployeeChangedTrigger({ connection, target, configuration: { changeType: 'Deleted' } })).toThrow();
  }, 30_000);

  it('decodes a Go-written inbox payload', () => {
    expect(decodeEmployeeChangedEventPayload('{"employeeId":"140","action":"Updated","lastChanged":"2026-10-13T09:00:01Z"}'))
      .toEqual({ employeeId: '140', action: 'Updated', lastChanged: '2026-10-13T09:00:01Z' });
    expect(() => decodeEmployeeChangedEventPayload('{"employeeId":140}')).toThrow();
  });
});
