// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GenerateTextInputSchema, newProjectConnection } from '../../src/index.js';
import { ConnectionName, Qihoo360GenerateTextFlow } from './flow.js';

const input = GenerateTextInputSchema.parse(JSON.parse(process.argv[2] || '{}') as object);
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const flow = new Qihoo360GenerateTextFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-qihoo360-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `qihoo360-inspection-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
