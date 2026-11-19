// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, AddressLookupFlow } from './flow.js';

const address = process.argv[2];
if (!address) throw new TypeError('usage: npm run example:start -- <structured Chinese address> [city]');
const project = await loadFromEnvironment();
const flow = new AddressLookupFlow(newProjectConnection(project, ConnectionName));
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-amap-example-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `amap-address-lookup-${randomUUID()}`;
  await client.startFlow(flow, flowId, { address, city: process.argv[3] });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
