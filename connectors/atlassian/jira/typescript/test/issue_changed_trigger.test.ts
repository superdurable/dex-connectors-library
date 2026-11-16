// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { newTriggerContext, SecretString, TriggerDeliveryAbortedError, type TriggerEvent } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import { decodeIssueChangeCursor, encodeIssueChangeCursor, newIssueChangeCursor, recentSeenVersions, type IssueChangeCursor } from '../src/issue_change_cursor.js';
import { newIssueChangedTriggerEvent } from '../src/issue_change_fetcher.js';
import { decodeIssue } from '../src/operations.js';
import {
  IssueChangedTriggerConfigurationSchema, IssueChangedTriggerDefinition, JiraClient, decodeIssueChangedEventPayload, defineIssueChangedTriggerBinding,
  newConnection, newIssueChangedTrigger, newIssueTriggerRunner, type IssueChangedEvent,
} from '../src/index.js';
import { CloudID, JiraSearchSite, Operations } from './jira_search_fake.js';

interface GoCases {
  encode: { position: Partial<IssueChangeCursor>; token: string }[];
  decode: { token: string; isValid: boolean; position?: Partial<IssueChangeCursor> }[];
  seenVersions: { previous: string[]; versions: [string, number][]; updatedThrough: number; kept: string[] }[];
  events: { resource: unknown; eventId: string; occurredAt: string; payload: string }[];
}
const goCases = JSON.parse(readFileSync(new URL('./fixtures/go-issue-change-cases.json', import.meta.url), 'utf8')) as GoCases;
const position = (value: Partial<IssueChangeCursor>): IssueChangeCursor => ({ ...newIssueChangeCursor('', '', 0), ...value, seenVersions: value.seenVersions ?? [] });
/** Go's int64 holds 2^63-1; a JavaScript number does not, so such a cursor reseeds, as the SDK's poll documents do. */
const beyondSafeInteger = '{"cloudId":"c","updatedThrough":9223372036854775807}';

describe('issueChanged cursor codec at parity with Go', () => {
  it.each(goCases.encode)('encodes $token', ({ position: value, token }) => {
    expect(encodeIssueChangeCursor(position(value)).token).toBe(token);
    expect(decodeIssueChangeCursor({ token })).toEqual(position(value));
  });

  it.each(goCases.decode)('decodes $token', ({ token, isValid, position: value }) => {
    if (token === beyondSafeInteger) { expect(() => decodeIssueChangeCursor({ token })).toThrow(TypeError); return; }
    if (!isValid) { expect(() => decodeIssueChangeCursor({ token })).toThrow(TypeError); return; }
    expect(decodeIssueChangeCursor({ token })).toEqual(position(value ?? {}));
  });

  it.each(goCases.seenVersions)('keeps the seen versions Go keeps', ({ previous, versions, updatedThrough, kept }) => {
    expect(recentSeenVersions(previous, versions, updatedThrough)).toEqual(kept);
  });

  it.each(goCases.events)('derives event $eventId and a Go-decodable payload', ({ resource, eventId, occurredAt, payload }) => {
    const event = newIssueChangedTriggerEvent(CloudID, decodeIssue(resource, [], false));
    expect(event.id).toBe(eventId);
    expect(event.occurredAt.getTime()).toBe(Date.parse(occurredAt));
    expect(event.payload).toEqual(JSON.parse(payload));
    expect(decodeIssueChangedEventPayload(payload, Buffer.from(payload))).toEqual(event.payload);
  });
});

describe('issueChanged configuration and wiring', () => {
  const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret'), accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const reference = { provider: 'atlassian', name: 'jira-trigger-test' };
  const connectionOn = (site: JiraSearchSite) => newConnection(new JiraClient({ cloudId: CloudID, endpoint: 'http://127.0.0.1:12345', pollIntervalMs: 5_000 }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: site.send }), reference);

  it('validates configurations strictly and declares the binding', () => {
    expect(IssueChangedTriggerConfigurationSchema.parse({})).toEqual({});
    expect(() => IssueChangedTriggerConfigurationSchema.parse({ projectId: 'OPS' })).toThrow('numeric');
    expect(() => IssueChangedTriggerConfigurationSchema.parse({ projectKey: 'ops' })).toThrow('uppercase');
    expect(() => IssueChangedTriggerConfigurationSchema.parse({ jql: 'project = OPS' })).toThrow();
    expect(() => decodeIssueChangedEventPayload('{}', Buffer.from('{}'))).toThrow();
    const binding = defineIssueChangedTriggerBinding({ connectionName: 'jira-issue-watcher', bindingName: 'changes', configurationUI: { units: [] } });
    expect(binding.definition).toEqual(IssueChangedTriggerDefinition);
  });

  it('records a durable baseline, then delivers through the inbox', async () => {
    const site = new JiraSearchSite();
    const connection = connectionOn(site);
    const objects = new InMemoryObjectStore();
    const scope = { projectId: 'jira-test', kind: 'live' as const };
    const key = { connectorId: 'jira', connectionName: reference.name, triggerName: 'issueChanged', bindingName: 'changes' };
    const cursor = newPollCursorStore(objects, scope, key);
    const delivered: TriggerEvent<IssueChangedEvent>[] = [];
    const target = newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, { handleTrigger: async (_context, event: TriggerEvent<IssueChangedEvent>) => { delivered.push(event); } }, { decodePayload: decodeIssueChangedEventPayload });
    site.create(Operations, 'Existing');
    const runner = newIssueTriggerRunner({ connection, issueChangedRoutes: [{ bindingName: 'changes', configuration: { projectKey: 'OPS' }, target, cursor }] });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    try {
      await waitUntil(async () => (await cursor.readPollCursor()).state !== undefined);
      await new Promise(resolve => setTimeout(resolve, 2));
      const created = site.create(Operations, 'Created after the baseline');
      runner.wakePoller('changes');
      await waitUntil(async () => delivered.length > 0);
      expect(delivered.map(event => event.payload.issue.key)).toEqual([created]);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token).project).toBe('key:OPS');
    } finally {
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });

  it('runs one binding as a TriggerRunner and rejects bad wiring', async () => {
    const site = new JiraSearchSite();
    const connection = connectionOn(site);
    const trigger = newIssueChangedTrigger({ connection, bindingName: 'changes', configuration: {}, target: { handleTrigger: async () => undefined } });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    await waitUntil(async () => site.requests().length > 0);
    stop.abort();
    expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(site.requests()[0]!.jql).toBe('updated >= -4m ORDER BY updated DESC');
    expect(() => newIssueTriggerRunner({ connection, issueChangedRoutes: [] })).toThrow('routes are required');
    expect(() => newIssueChangedTrigger({ connection, bindingName: 'changes', configuration: { projectId: 'OPS' }, target: { handleTrigger: async () => undefined } })).toThrow('numeric');
  });
});

async function waitUntil(condition: () => Promise<boolean>): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition not met within 10s');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}
