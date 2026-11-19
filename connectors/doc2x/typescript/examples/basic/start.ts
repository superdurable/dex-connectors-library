// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GetParseStatusInputSchema, newProjectConnection } from '../../src/index.js';
import { ConnectionName, Doc2xInspectionFlow } from './flow.js';

const input = GetParseStatusInputSchema.parse(JSON.parse(process.argv[2] || '{}'));
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const flow = new Doc2xInspectionFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-doc2x-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `doc2x-inspection-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
