// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListMeetingsInputSchema, newProjectConnection, type MeetingPage } from '../../src/index.js';
import { ConnectionName, MeetingListFlow } from './flow.js';

async function start(): Promise<void> {
  const file = process.argv[2];
  if (!file) throw new TypeError('Zoom meeting list input JSON file is required');
  const input = ListMeetingsInputSchema.parse(JSON.parse(await readFile(file, 'utf8')) as unknown);
  const project = await loadFromEnvironment();
  const flow = new MeetingListFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-zoom-meeting-list-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `zoom-list-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Zoom meeting list Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<MeetingPage>())}\n`);
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}
try { await start(); } catch { console.error('Zoom meeting list Flow could not start or complete.'); process.exitCode = 1; }
