// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runProfileChangeWorker } from '../../examples/profile-changed/app.js';
import { ConnectionName, ProfileChangedBinding, resolveFlowId, type ProfileChangeInput } from '../../examples/profile-changed/flow.js';
import type { ProfileChangedEvent } from '../../src/index.js';

const key = { connectorId: 'linkedin', connectionName: ConnectionName, triggerName: 'profileChanged', bindingName: ProfileChangedBinding };
const scope = { projectId: 'linkedin-profile-real-dex', kind: 'live' } as const;
async function newProject(objects?: InMemoryObjectStore) {
  const storage = objects ?? new InMemoryObjectStore();
  if (!objects) await new ConnectionStore(storage, scope).replaceCredential({ connectorId: 'linkedin', connectionName: ConnectionName }, 0, new CredentialMaterial({ access_token: 'one-use-token' }, undefined, 'oauth2'));
  return { project: new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1,
    connections: [{ connectorId: 'linkedin', connectionName: ConnectionName, modulePath: 'linkedin', provider: 'linkedin', configuration: { userInfoUrl: 'http://127.0.0.1/v2/userinfo', pollInterval: 60_000_000_000 } }],
    triggerBindings: [{ ...key, configuration: {} }], operationConfigurations: [],
  }, undefined, storage, undefined, undefined), objects: storage };
}
function eventually(condition: () => Promise<boolean> | boolean): Promise<void> {
  return expect.poll(condition, { timeout: 30_000, interval: 20 }).toBe(true);
}

it('starts one Flow after a profile change, replays after restart, and filters the unchanged profile through real Dex', async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  let name = 'Ada';
  const events: Array<TriggerEvent<ProfileChangedEvent>> = [];
  const send = async () => ({ status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify({ sub: 'member-real-dex', name, email_verified: false })) });
  const start = async (objects?: InMemoryObjectStore) => {
    const { project, objects: shared } = await newProject(objects);
    const stop = new AbortController();
    let ready!: (value: { client: Client; runner: { wakePoller(name: string): void } }) => void;
    const started = new Promise<{ client: Client; runner: { wakePoller(name: string): void } }>(resolve => { ready = resolve; });
    const running = runProfileChangeWorker(project, { serverAddress, linkedinOptions: { send }, onReady: ready, onFlowStarted: event => { events.push(event); } }, stop.signal);
    const worker = await Promise.race([started, running.then(() => { throw new Error('Worker stopped before ready'); })]);
    return { ...worker, project, objects: shared, stop: async () => { stop.abort(); await running; } };
  };
  const first = await start();
  await eventually(async () => !!(await first.project.pollCursorStore(key).readPollCursor()).state);
  expect(events).toHaveLength(0);
  name = 'Ada King';
  first.runner.wakePoller(ProfileChangedBinding);
  await eventually(() => events.length === 1);
  const firstResult = await first.client.waitForFlow(resolveFlowId(events[0]!), 60_000);
  expect(firstResult.status, firstResult.errorMessage).toBe('completed');
  expect(firstResult.singleOutput<ProfileChangeInput>().change.changedFields).toEqual(['name']);
  await first.stop();

  name = 'Ada Lovelace';
  const second = await start(first.objects);
  try {
    second.runner.wakePoller(ProfileChangedBinding);
    await eventually(() => events.length === 2);
    const secondResult = await second.client.waitForFlow(resolveFlowId(events[1]!), 60_000);
    expect(secondResult.status, secondResult.errorMessage).toBe('completed');
    expect(secondResult.singleOutput<ProfileChangeInput>().change.current.name).toBe(name);
    second.runner.wakePoller(ProfileChangedBinding);
    await eventually(async () => (await second.project.triggerInbox(key).pending()).length === 0);
    expect(events).toHaveLength(2);
  } finally { await second.stop(); }
}, 150_000);
