// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GetVideoInputSchema, newProjectConnection, type Video } from '@superdurable/connector-google-youtube';
import { ConnectionName, VideoLookupFlow } from './flow/workflow.js';

async function startVideoLookup(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('YouTube video input JSON file is required');
  const input = GetVideoInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new VideoLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-youtube-video-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `youtube-video-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('YouTube video Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<Video>())}\n`);
  } finally { try { await client?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); } }
}
try { await startVideoLookup(); } catch { console.error('YouTube video Flow could not start or complete.'); process.exitCode = 1; }
