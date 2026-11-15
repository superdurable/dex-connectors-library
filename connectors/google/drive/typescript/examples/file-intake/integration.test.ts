// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { loadFromEnvironment, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { newProjectConnection, type FileEvent } from '../../src/index.js';
import { DriveChangeLog } from '../../test/drive_change_log.js';
import { newFileIntakeRunner, newFileIntakeTarget } from './app.js';
import { ConnectionName, FileIntakeFlow, NewFileTriggerBinding, resolveFlowId, type FileIntakeOutcome } from './flow/workflow.js';

const accessToken = 'drive-trigger-integration-token';
const watchedFolderId = 'fld_inbox';

/** Write the owner-only connections.json that Dex Web saves for the connection and binding. */
async function writeProject(directory: string, endpoint: string): Promise<string> {
  const path = join(directory, 'connections.json');
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{
      connectorId: 'google-drive', authMethodId: 'google-oauth', provider: 'google', connectionName: ConnectionName,
      modulePath: 'github.com/superdurable/dex-connectors-library/connectors/google/drive/go',
      configuration: { endpoint, pollInterval: 1_000_000_000 },
      credentials: { oauth_client_id: 'client.apps.googleusercontent.com', oauth_client_secret: 'client-secret', access_token: accessToken, refresh_token: 'refresh-token' },
      credentialExpiresAt: new Date(Date.now() + 3_600_000).toISOString(),
    }],
    triggerBindings: [{
      connectorId: 'google-drive', connectionName: ConnectionName, triggerName: 'fileCreated', bindingName: NewFileTriggerBinding,
      configuration: { folderId: watchedFolderId, folderName: 'Inbox', mimeType: 'application/pdf' },
    }],
  }), { mode: 0o600 });
  await chmod(path, 0o600);
  return path;
}

/** Counts the Flow target's successful deliveries per event. */
class CountingTarget implements TriggerTarget<FileEvent> {
  readonly deliveries: Record<string, number> = {};
  constructor(private readonly target: TriggerTarget<FileEvent>) {}
  async handleTrigger(context: TriggerContext, event: TriggerEvent<FileEvent>): Promise<void> {
    await this.target.handleTrigger(context, event);
    this.deliveries[event.id] = (this.deliveries[event.id] ?? 0) + 1;
  }
}

async function eventually(condition: () => boolean, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error('condition was not met in time');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

/** Run the durable runner exactly as app.ts builds it while work runs, then stop it. */
async function runUntil(project: LoadedProject, target: TriggerTarget<FileEvent>, work: () => Promise<void>): Promise<void> {
  const stop = new AbortController();
  const running = newFileIntakeRunner(project, target).run(newTriggerContext(stop.signal));
  try { await work(); }
  finally { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); }
}

const flowIdOf = (fileId: string) => resolveFlowId({ id: '', occurredAt: new Date(), payload: { fileId } as FileEvent });

/** Poll until the Trigger started the file's Flow, then wait for its outcome. */
async function waitForIntake(client: Client, fileId: string): Promise<FileIntakeOutcome> {
  const flowId = flowIdOf(fileId);
  const deadline = Date.now() + 60_000;
  for (;;) {
    try { await client.describeFlow(flowId); break; }
    catch (error) { if (Date.now() > deadline) throw error; await new Promise(resolve => setTimeout(resolve, 50)); }
  }
  const result = await client.waitForFlow(flowId, 60_000);
  expect(result.status, result.errorMessage).toBe('completed');
  return result.singleOutput<FileIntakeOutcome>();
}

it('starts one Flow per created file across a runner restart with a real Dex Worker', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const runId = randomUUID().slice(0, 12);
  const fileId = (name: string) => `${name}_${runId}`;
  const changeLog = await new DriveChangeLog(accessToken).start();
  const hourAgo = Date.now() - 3_600_000;
  changeLog.put({ id: fileId('backlog'), name: 'Backlog.pdf', mimeType: 'application/pdf', parent: watchedFolderId, createdTime: hourAgo, modifiedTime: hourAgo });
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-drive-file-intake-test-'));
  // The project outlives each runner, as a replica's project storage does.
  const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: await writeProject(directory, changeLog.url) });
  const flow = new FileIntakeFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  const cache = openBlobCache({ directory: join(directory, 'blobs'), maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const target = new CountingTarget(newFileIntakeTarget(client, flow));
    await runUntil(project, target, async () => {
      await eventually(() => changeLog.listPages() >= 1);
      changeLog.createFile(fileId('invoice'), 'Invoice.pdf', 'application/pdf', watchedFolderId);
      changeLog.createFile(fileId('notes'), 'Notes', 'application/vnd.google-apps.document', watchedFolderId);
      changeLog.createFile(fileId('elsewhere'), 'Elsewhere.pdf', 'application/pdf', 'fld_other');
      changeLog.modifyFile(fileId('backlog'), Date.now());
      const invoice = await waitForIntake(client, fileId('invoice'));
      expect(invoice.eventId).toBe(`${fileId('invoice')}@created`);
      expect(invoice.reported).toMatchObject({ name: 'Invoice.pdf', parents: [watchedFolderId] });
      expect(invoice.current?.id).toBe(fileId('invoice'));
    });
    // A file created while the runner is stopped starts its Flow after the restart.
    changeLog.createFile(fileId('scan'), 'Scan.pdf', 'application/pdf', watchedFolderId);
    await runUntil(project, target, async () => {
      expect((await waitForIntake(client, fileId('scan'))).reported.name).toBe('Scan.pdf');
      const pages = changeLog.listPages();
      await eventually(() => changeLog.listPages() >= pages + 2);
    });
    for (const filtered of ['backlog', 'notes', 'elsewhere']) await expect(client.describeFlow(flowIdOf(fileId(filtered)))).rejects.toThrow();
    expect(target.deliveries).toEqual({ [`${fileId('invoice')}@created`]: 1, [`${fileId('scan')}@created`]: 1 });
    expect(changeLog.requests('/drive/v3/changes/startPageToken'), 'the restart resumed the stored cursor').toBe(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await changeLog.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 180_000);
