// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, GetEntryFlow, ListEntriesFlow } from './flow.js';

const spaceId = process.argv[2];
const entryId = process.argv[3];
if (!spaceId || !entryId) throw new TypeError('usage: npm run example:start -- <spaceId> <entryId> [environmentId]');
const environmentId = process.argv[4] || 'master';
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const getEntryFlow = new GetEntryFlow(connection);
const listEntriesFlow = new ListEntriesFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-contentful-example-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([getEntryFlow, listEntriesFlow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `contentful-get-${randomUUID()}`;
  await client.startFlow(getEntryFlow, flowId, { spaceId, environmentId, entryId });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
