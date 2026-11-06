// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newZohoProjectConnection } from '@superdurable/connector-zoho-crm';
import { ConnectionName, ZohoRecordLookupFlow } from './flow.js';

const [moduleName, recordId] = process.argv.slice(2);
if (!moduleName || !recordId) throw new TypeError('usage: npm run example:start -- MODULE RECORD_ID');
const project = await loadFromEnvironment();
const flow = new ZohoRecordLookupFlow(newZohoProjectConnection(project, ConnectionName));
const registry = new Registry([flow]);
const directory = await mkdtemp(join(tmpdir(), 'zoho-crm-start-cache-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
try {
  const flowId = `zoho-crm-lookup-${randomUUID()}`;
  await client.startFlow(flow, flowId, { module: moduleName, recordId });
  const result = await client.waitForFlow(flowId, 30_000);
  process.stdout.write(`${JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined })}\n`);
} finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
