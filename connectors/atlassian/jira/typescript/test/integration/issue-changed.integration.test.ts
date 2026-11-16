// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { runIssueChangedApp } from '../../examples/issue-changed/app.js';
import { ConnectionName, IssueChangedTriggerBinding, resolveFlowId, type IssueChangeOutcome } from '../../examples/issue-changed/flow/workflow.js';
import { issueChangeEventId, type Issue } from '../../src/index.js';
import { CloudID, HumanResources, JiraSearchSite, Operations } from '../jira_search_fake.js';

const scope = { projectId: 'jira-issue-changed', kind: 'live' } as const;
const opsProject = { projectId: Operations.id, projectKey: 'OPS', projectName: 'Operations' };
const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

/** Project storage that survives a process restart; each call builds a new LoadedProject over it. */
async function newTestProject(objects: InMemoryObjectStore, configuration: Record<string, unknown> | undefined, isFirstStart: boolean): Promise<LoadedProject> {
  if (isFirstStart) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'jira', connectionName: ConnectionName }, 0, new CredentialMaterial(
    { oauth_client_id: 'client-id', oauth_client_secret: 'client-secret', access_token: 'jira-token', refresh_token: 'refresh-token' }, new Date(Date.now() + 86_400_000), 'oauth2'));
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'jira', connectionName: ConnectionName, modulePath: 'jira', provider: 'atlassian', configuration: { cloudId: CloudID, pollInterval: 5_000_000_000 } }],
    triggerBindings: configuration ? [{ connectorId: 'jira', connectionName: ConnectionName, triggerName: 'issueChanged', bindingName: IssueChangedTriggerBinding, configuration }] : [],
  }, undefined, objects, undefined, undefined);
}

function newRecordingLogger(): TriggerLogger & { records: { message: string; attributes: TriggerLogAttributes }[] } {
  const records: { message: string; attributes: TriggerLogAttributes }[] = [];
  return { records, isEnabled: () => true, log: (_level: TriggerLogLevel, message, attributes) => { records.push({ message, attributes }); } };
}

/** One run of the example application; stopping it models a process exit. */
function startApp(project: LoadedProject, site: JiraSearchSite) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveReady!: (ready: { client: Client; wakePoller(): void }) => void;
  const ready = new Promise<{ client: Client; wakePoller(): void }>(resolve => { resolveReady = resolve; });
  const running = runIssueChangedApp(project, { serverAddress: dexAddress(), logger, jiraOptions: { send: site.send }, onReady: resolveReady }, stop.signal);
  const seeded = () => logger.records.some(record => record.message === 'poll cursor seeded');
  return { logger, ready, seeded, stop: async () => { stop.abort(); await running; } };
}

/** The Flow ID the current version of key starts. */
function flowIdOf(site: JiraSearchSite, key: string): string {
  const version = site.issueVersion(key);
  return resolveFlowId({ id: issueChangeEventId(CloudID, { id: version.id, updatedAt: version.updatedAt } as Issue), occurredAt: new Date(), payload: undefined as never });
}

async function waitForOutcome(client: Client, flowId: string): Promise<IssueChangeOutcome> {
  await expect.poll(async () => client.describeFlow(flowId).then(() => true, () => false), { timeout: 30_000, interval: 100 }).toBe(true);
  const result = await client.waitForFlow(flowId, 60_000);
  expect(result.status, result.errorMessage).toBe('completed');
  return result.singleOutput<IssueChangeOutcome>();
}

const delivered = (logger: ReturnType<typeof newRecordingLogger>) => logger.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate !== true).length;
const waitForNextMillisecond = () => new Promise(resolve => setTimeout(resolve, 2));

it('starts one Flow per issue version across a restart and never delivers the baseline', async () => {
  const site = new JiraSearchSite();
  const objects = new InMemoryObjectStore();
  const standup = site.create(Operations, 'Standup notes');
  const oldReview = site.create(Operations, 'Old review');
  const first = startApp(await newTestProject(objects, opsProject, true), site);
  try {
    await first.ready;
    await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true);
    const baselineFlow = flowIdOf(site, oldReview);
    await waitForNextMillisecond();
    const kickoff = site.create(Operations, 'Kickoff');
    site.update(standup, 'Standup notes, edited');
    site.create(HumanResources, 'Other project');
    (await first.ready).wakePoller();
    const { client } = await first.ready;
    const kickoffOutcome = await waitForOutcome(client, flowIdOf(site, kickoff));
    expect([kickoffOutcome.changeKind, kickoffOutcome.current.summary]).toEqual(['created', 'Kickoff']);
    expect((await waitForOutcome(client, flowIdOf(site, standup))).current.summary, 'the Flow reads the issue back').toBe('Standup notes, edited');
    await expect(client.describeFlow(baselineFlow)).rejects.toThrow();
    expect(delivered(first.logger)).toBe(2);
  } finally { await first.stop(); }

  await waitForNextMillisecond();
  const offline = site.create(Operations, 'Filed while stopped');
  site.update('OPS-3', 'Kickoff, rescheduled');
  const second = startApp(await newTestProject(objects, opsProject, false), site);
  try {
    const { client, wakePoller } = await second.ready;
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(site, offline))).current.summary).toBe('Filed while stopped');
    expect((await waitForOutcome(client, flowIdOf(site, 'OPS-3'))).current.summary, 'the second version starts its own Flow').toBe('Kickoff, rescheduled');
    const searchesBefore = site.requests().length;
    wakePoller();
    await expect.poll(() => site.requests().length, { timeout: 30_000 }).toBeGreaterThan(searchesBefore);
    expect(delivered(second.logger), 'no version is delivered twice').toBe(2);
  } finally { await second.stop(); }
}, 180_000);

it('pauses on a 429 with Retry-After, then delivers without losing a change', async () => {
  const site = new JiraSearchSite();
  const app = startApp(await newTestProject(new InMemoryObjectStore(), opsProject, true), site);
  try {
    const { client, wakePoller } = await app.ready;
    await expect.poll(app.seeded, { timeout: 30_000 }).toBe(true);
    site.rateLimitNextRequests(1, '1');
    const created = site.create(Operations, 'Filed during a rate limit');
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(site, created))).current.summary).toBe('Filed during a rate limit');
    expect(site.requests().map(request => request.status)).toContain(429);
    expect(app.logger.records.some(record => record.message === 'poll rate limited; pausing connection')).toBe(true);
  } finally { await app.stop(); }
}, 120_000);

it('records a new baseline when the binding watches another project', async () => {
  const site = new JiraSearchSite();
  const objects = new InMemoryObjectStore();
  const first = startApp(await newTestProject(objects, opsProject, true), site);
  try { await first.ready; await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true); } finally { await first.stop(); }
  const gap = site.create(HumanResources, 'Existing in the new project');
  const project = await newTestProject(objects, { projectKey: 'HR' }, false);
  const second = startApp(project, site);
  try {
    const { client, wakePoller } = await second.ready;
    const cursor = project.pollCursorStore({ connectorId: 'jira', connectionName: ConnectionName, triggerName: 'issueChanged', bindingName: IssueChangedTriggerBinding });
    await expect.poll(async () => (await cursor.readPollCursor()).state?.resetCount, { timeout: 30_000 }).toBe(1);
    expect(second.logger.records.some(record => record.message === 'poll cursor invalid; reseeding')).toBe(true);
    await waitForNextMillisecond();
    const after = site.create(HumanResources, 'After the reset');
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(site, after))).current.summary).toBe('After the reset');
    await expect(client.describeFlow(flowIdOf(site, gap)), "the new project's existing issues are its baseline").rejects.toThrow();
  } finally { await second.stop(); }
}, 120_000);
