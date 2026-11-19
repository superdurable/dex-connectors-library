// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runProfileChangeWorker } from '../../examples/profile-changed/app.js';
import { ConnectionName, ProfileChangedTriggerBinding, resolveFlowId, type ProfileChangeOutcome } from '../../examples/profile-changed/flow.js';
import type { ProfileChangedEvent } from '../../src/index.js';
import { LinkedInProfileFake, adaProfile, newTestProject } from '../linkedin_profile_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'linkedin', connectionName: ConnectionName, triggerName: 'profileChanged', bindingName: ProfileChangedTriggerBinding };

/** One Worker and Trigger process of the example over shared project storage. */
async function startProcess(fake: LinkedInProfileFake, delivered: Map<string, TriggerEvent<ProfileChangedEvent>>, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runProfileChangeWorker(project, {
    serverAddress: dexAddress(), linkedInOptions: { send: fake.send }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForCommits(project: LoadedProject, commitCount: number): Promise<void> {
  await expect.poll(async () => (await cursorState(project))?.commitCount ?? 0, { timeout: 30_000 }).toBeGreaterThanOrEqual(commitCount);
}

async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<ProfileChangedEvent>>, count: number): Promise<ProfileChangeOutcome[]> {
  await expect.poll(() => delivered.size, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes: ProfileChangeOutcome[] = [];
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    outcomes.push(result.singleOutput<ProfileChangeOutcome>());
  }
  return outcomes;
}

// A unique member per run keeps Flow IDs unique on a reused Dex server.
const uniqueSubject = () => `ts${Date.now().toString(36)}${Math.floor(Math.random() * 1e6).toString(36)}`;

it('starts one Flow per profile change across a restart with real Dex', async () => {
  const fake = new LinkedInProfileFake({ ...adaProfile(), sub: uniqueSubject() }), delivered = new Map<string, TriggerEvent<ProfileChangedEvent>>();
  const first = await startProcess(fake, delivered);
  await waitForCommits(first.project, 1);
  fake.update(profile => { profile.name = 'Ada King'; profile.family_name = 'King'; });
  first.runner.wakePoller(ProfileChangedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 1);
  expect(outcomes[0]?.changedFields).toEqual(['name', 'familyName']);
  expect(outcomes[0]?.verifiedProfile?.name, 'the Flow reads the verified profile back').toBe('Ada King');
  await first.stop();

  fake.update(profile => { profile.email_verified = false; });
  const second = await startProcess(fake, delivered, first.objects);
  try {
    second.runner.wakePoller(ProfileChangedTriggerBinding);
    outcomes = await waitForOutcomes(second.client, delivered, 2);
    const unverified = outcomes.find(outcome => outcome.changedFields.includes('emailVerified'));
    expect(unverified, 'a change made while stopped arrives after the restart').toBeDefined();
    expect(unverified?.verifiedProfile, 'verifiedEmailRequired records no verified profile').toBeUndefined();
    await waitForCommits(second.project, 3);
    expect(delivered.size, 'no change twice and never the baseline').toBe(2);
  } finally { await second.stop(); }
}, 120_000);

it("records a new baseline when the connection now belongs to another member, with real Dex", async () => {
  const fake = new LinkedInProfileFake({ ...adaProfile(), sub: uniqueSubject() }), delivered = new Map<string, TriggerEvent<ProfileChangedEvent>>();
  const first = await startProcess(fake, delivered);
  await waitForCommits(first.project, 1);
  await first.stop();

  const otherMember = uniqueSubject();
  fake.update(profile => { profile.sub = otherMember; profile.name = 'Grace Hopper'; });
  const second = await startProcess(fake, delivered, first.objects);
  try {
    await expect.poll(async () => (await cursorState(second.project))?.resetCount, { timeout: 30_000 }).toBe(1);
    fake.update(profile => { profile.locale = 'fr-FR'; });
    second.runner.wakePoller(ProfileChangedTriggerBinding);
    const outcomes = await waitForOutcomes(second.client, delivered, 1);
    expect(outcomes.map(outcome => [outcome.subject, outcome.changedFields]), 'the other member is a new baseline').toEqual([[otherMember, ['locale']]]);
    expect(JSON.parse((await cursorState(second.project))!.token)).toMatchObject({ version: 1, subject: otherMember, sequence: 1 });
  } finally { await second.stop(); }
}, 120_000);
