// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, listMeetingsListed, listMeetingsRetry } from '../../src/mock.js';
import type { MeetingPage } from '../../src/index.js';
import { ConnectionName, MeetingListFlow } from './flow.js';

/** Verify the checked-in Flow through a real Dex Worker, Client, persistence, and retry boundary. */
export async function verifyMeetingList(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName), flow = new MeetingListFlow(mock.connection());
  const directory = await mkdtemp(join(tmpdir(), 'dex-zoom-meeting-list-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const registry = new Registry([flow]), worker = new Worker(registry, cache, { serverAddress }), client = new Client(registry, cache, { serverAddress });
  const page: MeetingPage = { meetings: [{ id: 92674392836, topic: 'Consultation' }], totalRecords: 1 };
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `zoom-list-${randomUUID()}`;
    mock.listMeetings().forFlow(flowId).respond(listMeetingsRetry({ kind: 'RATE_LIMIT', provider: 'zoom', operation: 'listMeetings', message: 'Zoom rate limited the request' }, 10), listMeetingsListed(page));
    await client.startFlow(flow, flowId, { type: 'upcoming', pageSize: 30 });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.deepEqual(result.singleOutput<MeetingPage>(), page);
    assert.equal(mock.listMeetings().calls().filter(call => call.flowId === flowId).length, 2);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) verifyMeetingList(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
