// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { LinkedInClient, newConnection, newProjectProfileTriggerRunner, type ProfileChangedEvent } from '../src/index.js';

const connectionName = 'linkedin-profile-watcher';
const bindingName = 'own-profile-changes';
const reference = { provider: 'linkedin', name: connectionName };
const key = { connectorId: 'linkedin', connectionName, triggerName: 'profileChanged', bindingName };
const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('one-use-token') }]]);
const scope = { projectId: 'linkedin-profile-trigger-test', kind: 'live' } as const;
function project(objects: InMemoryObjectStore, bindingConfiguration: Record<string, unknown> = {}): LoadedProject {
  return new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1,
    connections: [{ connectorId: 'linkedin', connectionName, modulePath: 'linkedin', provider: 'linkedin', configuration: {} }],
    triggerBindings: [{ ...key, configuration: bindingConfiguration }], operationConfigurations: [],
  }, undefined, objects, undefined, undefined);
}
async function eventually(condition: () => Promise<boolean> | boolean): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}
function run(runner: ReturnType<typeof newProjectProfileTriggerRunner>) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

it('commits after delivery, replays a failed inbox after restart, and deduplicates an unchanged poll', async () => {
  const objects = new InMemoryObjectStore();
  let name = 'Ada';
  const requests: string[] = [];
  const send = async () => {
    requests.push(name);
    return { status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify({ sub: 'member', name, email_verified: false })) };
  };
  const createRunner = (target: TriggerTarget<ProfileChangedEvent>) => {
    const loaded = project(objects);
    const connection = newConnection(new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo', pollIntervalMs: 60_000 }, credentials, { send }), reference);
    return { loaded, runner: newProjectProfileTriggerRunner(loaded, connection, [{ bindingName, target }]) };
  };
  let attempts = 0;
  const first = createRunner({ handleTrigger: async () => { attempts++; throw new Error('delivery unavailable'); } });
  const firstRun = run(first.runner);
  await eventually(async () => !!(await first.loaded.pollCursorStore(key).readPollCursor()).state);
  name = 'Ada King';
  first.runner.wakePoller(bindingName);
  await eventually(() => attempts >= 1);
  expect((await first.loaded.triggerInbox(key).pending()).length).toBe(1);
  await firstRun.stop();

  const delivered: Array<TriggerEvent<ProfileChangedEvent>> = [];
  const second = createRunner({ handleTrigger: async (_context, event) => { delivered.push(event); } });
  const secondRun = run(second.runner);
  try {
    await eventually(() => delivered.length === 1);
    expect(delivered[0]?.payload.changedFields).toEqual(['name']);
    await eventually(async () => (await second.loaded.triggerInbox(key).pending()).length === 0);
    second.runner.wakePoller(bindingName);
    await eventually(() => requests.length >= 3);
    expect(delivered).toHaveLength(1);
  } finally { await secondRun.stop(); }
}, 30_000);

it('rejects saved Trigger fields and invalid routes', () => {
  const objects = new InMemoryObjectStore();
  const connection = newConnection(new LinkedInClient({}, credentials), reference);
  expect(() => newProjectProfileTriggerRunner(project(objects, { memberId: 'another' }), connection, [{ bindingName, target: { handleTrigger: async () => {} } }])).toThrow('configuration must be empty');
  expect(() => newProjectProfileTriggerRunner(project(objects), connection, [])).toThrow('routes are required');
});
