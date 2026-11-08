// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, ContactInspectionFlow } from './flow.js';

const offset = Number(process.argv[2] || '0');
if (!Number.isSafeInteger(offset) || offset < 0) throw new TypeError('usage: npm run example:start -- [nonnegative offset]');
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const flow = new ContactInspectionFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-activecampaign-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `activecampaign-contacts-${randomUUID()}`;
  await client.startFlow(flow, flowId, { offset, limit: 20 });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
