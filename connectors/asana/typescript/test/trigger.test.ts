// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext, type ConnectionRef, type TriggerTarget } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import {
  Client, TaskChangedEventSchema, TaskChangedTriggerConfigurationSchema, TaskChangedTriggerDefinition, decodeTaskChangedEventPayload,
  newConnection, newProjectTaskTriggerRunner, newTaskChangedTrigger, newTaskTriggerRunner, validateTaskChangedTriggerConfiguration,
  type TaskChangedEvent,
} from '../src/index.js';
import { parseRawJSON } from '../src/event_json.js';
import { TaskEventFetcher } from '../src/task_event_fetcher.js';
import { AccessToken, AsanaEventsFake, Endpoint } from './asana_events_fake.js';

const projectId = '1201000000000001';
const reference: ConnectionRef = { provider: 'asana', name: 'asana-events' };
const trigger = TaskChangedTriggerDefinition.trigger;

function newClient(send: (request: ProviderRequest) => Promise<ProviderResponse>, pollIntervalMs = 5_000): Client {
  return new Client({ endpoint: Endpoint, pollIntervalMs }, { resolve: async () => ({ accessToken: new SecretString(AccessToken) }) }, { send });
}

function newFetcher(fake: AsanaEventsFake, watchedProject = projectId): TaskEventFetcher {
  return new TaskEventFetcher(newClient(fake.send), reference, trigger, watchedProject);
}

const context = () => newTriggerContext();
const recordingTarget: TriggerTarget<TaskChangedEvent> = { handleTrigger: async () => undefined };

describe('TaskEventFetcher', () => {
  it('seeds without backlog and delivers only task events with Go event IDs', async () => {
    const fake = new AsanaEventsFake(projectId);
    fake.addTask('1204000000000001', 'Existing');
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context());
    expect(seed.token).toBe('{"projectId":"1201000000000001","sync":"sync-1"}');
    expect(fake.requests[0]!.query.has('sync')).toBe(false);
    expect(fake.requests[0]!.status).toBe(412);

    fake.addTask('1204000000000002', 'Replace badge reader');
    fake.addStory('1205000000000001');
    fake.completeTask('1204000000000001');
    fake.deleteTask('1204000000000002');
    const page = await fetcher.fetchChanges(context(), seed);
    expect(page.hasMorePages).toBe(false);
    expect(page.next.token).toBe('{"projectId":"1201000000000001","sync":"sync-5"}');
    expect(page.changes).toHaveLength(3);
    const [added, completed, deleted] = page.changes.map(change => change.payload);
    expect(added).toEqual({
      projectId, action: 'added', task: { id: '1204000000000002', name: 'Replace badge reader' }, taskSubtype: 'default_task',
      parent: { id: projectId, resourceType: 'project', name: 'Facilities' }, user: { id: '1203000000000001', name: 'Ada Lovelace' },
      occurredAt: '2026-03-02T09:00:01Z',
    });
    expect([completed!.action, completed!.changedField, completed!.changeAction, completed!.parent]).toEqual(['changed', 'completed', 'changed', undefined]);
    expect(deleted!.action).toBe('deleted');
    expect(page.changes[0]!.occurredAt.toISOString()).toBe('2026-03-02T09:00:01.000Z');
    // The Go fetcher derives these IDs from the same events, so Go and TypeScript replicas share an inbox.
    expect(page.changes.map(change => change.id)).toEqual([
      'asana-94ef95efc2e9cbae0b7708d10be4af34', 'asana-d60917095242b62fe9a12fac1f4d905c', 'asana-39f1a4423dba4c43eba1bb14521a82df',
    ]);
    const query = fake.requests[1]!.query;
    expect([query.get('sync'), query.get('resource')]).toEqual(['sync-1', projectId]);
    expect(query.get('opt_fields')).toContain('change.field');
    const again = await fetcher.fetchChanges(context(), seed);
    expect(again.changes.map(change => change.id)).toEqual(page.changes.map(change => change.id));
  });

  it('follows has_more in pages of one hundred', async () => {
    const fake = new AsanaEventsFake(projectId);
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context());
    for (let index = 0; index < 150; index++) fake.addTask(String(1204000000001000 + index), 'Bulk task');
    const first = await fetcher.fetchChanges(context(), seed);
    expect([first.changes.length, first.hasMorePages]).toEqual([100, true]);
    const second = await fetcher.fetchChanges(context(), first.next);
    expect([second.changes.length, second.hasMorePages]).toEqual([50, false]);
  });

  it('marks expired and foreign cursors invalid', async () => {
    const fake = new AsanaEventsFake(projectId);
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context());
    fake.addTask('1204000000000003', 'Lost in the gap');
    fake.expireSyncTokens();
    const expired = await fetcher.fetchChanges(context(), seed).catch((error: unknown) => error);
    expect(isCursorInvalid(expired)).toBe(true);
    expect(String(expired)).toContain('412');
    for (const token of ['{"projectId":"1201000000000999","sync":"sync-0"}', 'not json', 'null', '{"sync":""}']) {
      expect(isCursorInvalid(await fetcher.fetchChanges(context(), { token }).catch((error: unknown) => error)), token).toBe(true);
    }
  });

  it('reports rate limits with Retry-After and other failures as retryable polls', async () => {
    const fake = new AsanaEventsFake(projectId);
    fake.rateLimitNext(1, '7');
    const limited = await newFetcher(fake).seedCursor(context()).catch((error: unknown) => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7_000);
    const rejected = await newFetcher(fake, '1201000000000999').seedCursor(context()).catch((error: unknown) => error);
    expect(String(rejected)).toContain('HTTP 404');
    expect(isCursorInvalid(rejected) || rejected instanceof RateLimitedError).toBe(false);
  });

  it.each([
    [200, '[1]', 'not valid JSON'],
    [200, '{"data":[{"action":7}],"sync":"sync-2"}', 'not valid JSON'],
    [200, '{"data":[],"sync":"sync-2",}', 'not valid JSON'],
    [200, '{"data":[]}', 'no sync token'],
    [200, JSON.stringify({ data: Array.from({ length: 101 }, () => ({})), sync: 'sync-2' }), '101 events'],
    [412, '{}', '412 without a fresh sync token'],
    [500, '{}', 'HTTP 500'],
  ])('rejects a malformed %i page %s', async (status, body, message) => {
    const fetcher = new TaskEventFetcher(newClient(async () => ({ status, headers: {}, body: Buffer.from(body) })), reference, trigger, projectId);
    const failed = await fetcher.fetchChanges(context(), { token: '{"projectId":"1201000000000001","sync":"sync-1"}' }).catch((error: unknown) => error);
    expect(String(failed)).toContain(message);
    expect(isCursorInvalid(failed)).toBe(false);
  });

  it('matches Go event IDs for raw change values, repeats, renames, and page boundaries', () => {
    const fetcher = new TaskEventFetcher(newClient(async () => { throw new Error('no request'); }), reference, trigger, projectId);
    const follower = '{"action":"changed","created_at":"2026-03-02T09:00:00Z","resource":{"gid":"1204000000000001","resource_type":"task"},'
      + '"change":{"field":"followers","action":"added","added_value":{"gid":"1203000000000002","name":"Grace"}}}';
    const raw = '[{"action":"changed","created_at":"2026-03-02T09:00:00Z","user":{"gid":"1203000000000002"},"parent":null,'
      + '"resource":{"gid":"1204000000000001","resource_type":"task","name":"Old"},'
      + '"change":{"field":"notes","action":"changed","new_value":"caf\\u00e9 <b> \\"x\\"","added_value":{ "gid" : 7 , "a" : [1, 2.50e1] },"removed_value":null}},'
      + `\n\t${follower},\n\t${follower}]`;
    const events = (text: string) => parseRawJSON(Buffer.from(text)).elements;
    const changes = fetcher.convertEvents('sync-7', events(raw));
    // Printed by the Go connector's convertEvents for the same bytes.
    expect(changes.map(change => change.id)).toEqual([
      'asana-8979723e6241192efead4ea7e22b2c8b', 'asana-356a8d7a9091992cd9fe60de0e75ce48', 'asana-4e2030d08703f58b3d49ace8e7b3a0ba',
    ]);
    expect(fetcher.convertEvents('sync-7', events(raw.replace('"name":"Old"', '"name":"New"')))[0]!.id).toBe(changes[0]!.id);
    expect(fetcher.convertEvents('sync-107', events(raw))[0]!.id).not.toBe(changes[0]!.id);
    expect(() => fetcher.convertEvents('sync-1', events('[{"action":"moved","resource":{"gid":"1","resource_type":"task"}}]'))).toThrow('unknown action');
  });

  it('matches Go for case-folded and repeated keys, escapes, and an HTML-escaped cursor', async () => {
    const page = readFileSync(new URL('./go_parity_page.json', import.meta.url));
    const fake = { send: async () => ({ status: 200, headers: {}, body: page }) };
    const fetcher = new TaskEventFetcher(newClient(fake.send), reference, trigger, '555');
    const changes = fetcher.convertEvents('req<&>\u2028', parseRawJSON(page).arrayMember('data'));
    // Printed by the Go connector's convertEvents and encodeCursor for the same bytes.
    expect(changes.map(change => change.id)).toEqual([
      'asana-ccc4d0ed986df566d6cb70b0dfbac239', 'asana-5de4c7e345ddad0544db6d4118e40efa', 'asana-73f43676a5fc5b5f33625b9b6b1f8073',
      'asana-436881099e03f0ad4014a13a36225578', 'asana-3e3b666dec2f900a239c745fc2ae3df4',
    ]);
    expect(changes[3]!.occurredAt.toISOString()).toBe('2026-11-01T08:00:01.000Z');
    const next = await fetcher.fetchChanges(context(), { token: '{"projectId":"555","sync":"s"}' });
    expect(next.next.token).toBe('{"projectId":"555","sync":"s\\u003c\\u0026\\u003e\\u2028\u00e9"}');
  });
});

describe('taskChanged configuration and runners', () => {
  it('requires a project gid and decodes saved configuration strictly', () => {
    validateTaskChangedTriggerConfiguration({ workspaceId: '1200000000000001', projectId });
    for (const invalid of [{ projectId: '' }, { projectId: 'Facilities' }, { projectId, workspaceId: 'ops' }]) {
      expect(() => validateTaskChangedTriggerConfiguration(invalid)).toThrow(TypeError);
    }
    expect(TaskChangedTriggerConfigurationSchema.parse({ projectId, workspaceId: null })).toEqual({ projectId });
    expect(() => TaskChangedTriggerConfigurationSchema.parse({ projectId, sectionId: '1' })).toThrow();
  });

  it('decodes a stored Go payload and ignores unknown fields', () => {
    const stored = '{"projectId":"1","action":"added","task":{"id":"2"},"occurredAt":"t","extra":1}';
    expect(decodeTaskChangedEventPayload(stored, Buffer.from(stored))).toEqual({ projectId: '1', action: 'added', task: { id: '2' }, occurredAt: 't' });
    expect(TaskChangedEventSchema.parse({ projectId: '1', action: 'deleted', task: { id: '2', name: '' }, parent: null, occurredAt: 't' })).toEqual({ projectId: '1', action: 'deleted', task: { id: '2' }, occurredAt: 't' });
  });

  it('rejects incomplete routes, short intervals, and bindings without a saved project', () => {
    const connection = newConnection(newClient(async () => { throw new Error('no request'); }), reference);
    const configuration = { projectId };
    for (const routes of [
      [], [{ bindingName: '', configuration, target: recordingTarget }],
      [{ bindingName: 'projectTasks', configuration: { projectId: '' }, target: recordingTarget }],
      [{ bindingName: 'projectTasks', configuration, target: recordingTarget }, { bindingName: 'projectTasks', configuration, target: recordingTarget }],
    ]) expect(() => newTaskTriggerRunner({ connection, taskChangedRoutes: routes })).toThrow();
    expect(() => newClient(async () => { throw new Error('no request'); }, 1_000)).toThrow('pollInterval must be at least 5s');
    expect(() => newTaskChangedTrigger({ connection, connectionName: 'asana-events', bindingName: 'b', configuration: { projectId: '' }, target: recordingTarget })).toThrow('projectId');
    const project = { configuration: {
      connections: [{ connectorId: 'asana', connectionName: 'asana-events', provider: 'asana', configuration: {} }], triggerBindings: [],
    } } as unknown as LoadedProject;
    expect(() => newProjectTaskTriggerRunner(project, 'asana-events', { taskChangedRoutes: [{ bindingName: 'projectTasks', target: recordingTarget }] })).toThrow('has no saved project');
  });

  it('matches the manifest and the generated descriptor', () => {
    const manifest = parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
    expect(manifest.spec.languages.typescript.triggers).toEqual(['taskChanged']);
    expect(TaskChangedTriggerDefinition.trigger.triggerName).toBe(manifest.spec.triggers[0].name);
    expect(TaskChangedTriggerDefinition.description).toBe(manifest.spec.triggers[0].description);
    const descriptor = JSON.parse(readFileSync(new URL('../dex-connector.json', import.meta.url), 'utf8'));
    const factories = descriptor.factories.filter((factory: { kind: string }) => factory.kind === 'trigger');
    expect(factories.map((factory: { name: string }) => factory.name)).toEqual(['defineTaskChangedTriggerBinding']);
  });
});
