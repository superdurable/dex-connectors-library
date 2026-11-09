// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, MediaListFlow } from './flow.js';

const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const flow = new MediaListFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-instagram-business-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const igUserId = process.env.META_INSTAGRAM_USER_ID;
  if (!igUserId || !/^[0-9]{1,40}$/.test(igUserId)) throw new TypeError('META_INSTAGRAM_USER_ID must be a numeric Instagram professional account ID');
  const flowId = `instagram-business-media-${randomUUID()}`;
  await client.startFlow(flow, flowId, { igUserId });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
