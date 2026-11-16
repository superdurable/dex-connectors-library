// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import type { Client } from '@superdurable/dex';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { runPageChangedApp } from '../../examples/page-changed/app.js';
import { ConnectionName, PageChangedTriggerBinding, resolveFlowId, type PageChangeOutcome } from '../../examples/page-changed/flow/workflow.js';
import { pageChangeEventId } from '../../src/index.js';
import { AccessToken, ConfluencePageSite } from '../confluence_page_fake.js';

const scope = { projectId: 'confluence-page-changed', kind: 'live' } as const;
const opsSpace = { spaceId: '98306', spaceKey: 'OPS', spaceName: 'Operations' };
const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

/** Project storage that survives a process restart; each call builds a new LoadedProject over it. */
async function newTestProject(objects: InMemoryObjectStore, configuration: Record<string, unknown> | undefined, isFirstStart: boolean): Promise<LoadedProject> {
  if (isFirstStart) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'confluence', connectionName: ConnectionName }, 0, new CredentialMaterial(
    { oauth_client_id: 'client-id', oauth_client_secret: 'client-secret', access_token: AccessToken, refresh_token: 'refresh-token' }, new Date(Date.now() + 86_400_000), 'oauth2'));
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'confluence', connectionName: ConnectionName, modulePath: 'confluence', provider: 'atlassian', configuration: { endpoint: 'http://127.0.0.1:12345', pollInterval: 10_000_000_000 } }],
    triggerBindings: configuration ? [{ connectorId: 'confluence', connectionName: ConnectionName, triggerName: 'pageChanged', bindingName: PageChangedTriggerBinding, configuration }] : [],
  }, undefined, objects, undefined, undefined);
}

function newRecordingLogger(): TriggerLogger & { records: { message: string; attributes: TriggerLogAttributes }[] } {
  const records: { message: string; attributes: TriggerLogAttributes }[] = [];
  return { records, isEnabled: () => true, log: (_level: TriggerLogLevel, message, attributes) => { records.push({ message, attributes }); } };
}

/** One run of the example application; stopping it models a process exit. */
function startApp(project: LoadedProject, site: ConfluencePageSite) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveReady!: (ready: { client: Client; wakePoller(): void }) => void;
  const ready = new Promise<{ client: Client; wakePoller(): void }>(resolve => { resolveReady = resolve; });
  const running = runPageChangedApp(project, { serverAddress: dexAddress(), logger, confluenceOptions: { send: site.send }, onReady: resolveReady }, stop.signal);
  const seeded = () => logger.records.some(record => record.message === 'poll cursor seeded');
  return { logger, ready, seeded, stop: async () => { stop.abort(); await running; } };
}

/** The Flow ID one page version starts; a fresh site ID per test keeps Flow IDs unique across runs. */
function flowIdOf(site: ConfluencePageSite, pageId: string, versionNumber: number): string {
  return resolveFlowId({ id: pageChangeEventId(site.cloudId, pageId, versionNumber), occurredAt: new Date(), payload: undefined as never });
}

async function waitForOutcome(client: Client, flowId: string): Promise<PageChangeOutcome> {
  await expect.poll(async () => client.describeFlow(flowId).then(() => true, () => false), { timeout: 30_000, interval: 100 }).toBe(true);
  const result = await client.waitForFlow(flowId, 60_000);
  expect(result.status, result.errorMessage).toBe('completed');
  return result.singleOutput<PageChangeOutcome>();
}

const delivered = (logger: ReturnType<typeof newRecordingLogger>) => logger.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate !== true).length;
const waitForNextMillisecond = () => new Promise(resolve => setTimeout(resolve, 2));

it('starts one Flow per page version across a restart and never delivers the baseline', async () => {
  const site = new ConfluencePageSite(randomUUID());
  const objects = new InMemoryObjectStore();
  const existing = site.publish('OPS', 'Existing policy', 'Before the Trigger started');
  const first = startApp(await newTestProject(objects, opsSpace, true), site);
  let created: string;
  try {
    const { client, wakePoller } = await first.ready;
    await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true);
    await waitForNextMillisecond();
    created = site.publish('OPS', 'Remote work policy', 'Two days a week');
    site.edit(existing, 'Existing policy', 'Revised');
    site.publish('HR', 'Other space', 'Ignored');
    wakePoller();
    const createdOutcome = await waitForOutcome(client, flowIdOf(site, created, 1));
    expect([createdOutcome.changeKind, createdOutcome.body], 'the Flow reads the page back as Markdown').toEqual(['created', 'Two days a week']);
    expect((await waitForOutcome(client, flowIdOf(site, existing, 2))).changeKind).toBe('updated');
    await expect(client.describeFlow(flowIdOf(site, existing, 1))).rejects.toThrow();
    expect(delivered(first.logger)).toBe(2);
  } finally { await first.stop(); }

  await waitForNextMillisecond();
  const offline = site.publish('OPS', 'Saved while stopped', 'Offline');
  site.edit(created, 'Remote work policy', 'Three days a week');
  site.throttleSearches(1, 1);
  const second = startApp(await newTestProject(objects, opsSpace, false), site);
  try {
    const { client, wakePoller } = await second.ready;
    expect((await waitForOutcome(client, flowIdOf(site, offline, 1))).title).toBe('Saved while stopped');
    expect((await waitForOutcome(client, flowIdOf(site, created, 2))).body, 'the second version starts its own Flow').toBe('Three days a week');
    expect(site.searchStatuses, "the restart's first search was rate limited").toContain(429);
    const searchesBefore = site.searchStatuses.length;
    wakePoller();
    await expect.poll(() => site.searchStatuses.length, { timeout: 30_000 }).toBeGreaterThan(searchesBefore);
    expect(delivered(second.logger), 'no version is delivered twice').toBe(2);
  } finally { await second.stop(); }
}, 180_000);

it('records a new baseline when the binding watches another space', async () => {
  const site = new ConfluencePageSite(randomUUID());
  const objects = new InMemoryObjectStore();
  const first = startApp(await newTestProject(objects, opsSpace, true), site);
  try { await first.ready; await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true); } finally { await first.stop(); }
  const existing = site.publish('HR', 'Existing in the new space', 'Baseline');
  const project = await newTestProject(objects, { spaceKey: 'HR' }, false);
  const second = startApp(project, site);
  try {
    const { client, wakePoller } = await second.ready;
    const cursor = project.pollCursorStore({ connectorId: 'confluence', connectionName: ConnectionName, triggerName: 'pageChanged', bindingName: PageChangedTriggerBinding });
    await expect.poll(async () => (await cursor.readPollCursor()).state?.resetCount, { timeout: 30_000 }).toBe(1);
    expect(second.logger.records.some(record => record.message === 'poll cursor invalid; reseeding')).toBe(true);
    await waitForNextMillisecond();
    const after = site.publish('HR', 'After the reset', 'New');
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(site, after, 1))).title).toBe('After the reset');
    await expect(client.describeFlow(flowIdOf(site, existing, 1)), "the new space's existing pages are its baseline").rejects.toThrow();
  } finally { await second.stop(); }
}, 120_000);
