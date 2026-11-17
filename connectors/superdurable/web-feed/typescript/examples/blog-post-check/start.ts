// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { BlogPostCheck, ConnectionName, type CheckRequest } from './flow.js';

const request = JSON.parse(process.argv[2] || '{"siteUrl":"https://go.dev/blog/","maxPosts":3}') as CheckRequest;
const project = await loadFromEnvironment();
const flow = new BlogPostCheck(newProjectConnection(project, ConnectionName));
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-web-feed-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `blog-post-check-${randomUUID()}`;
  await client.startFlow(flow, flowId, request);
  const result = await client.waitForFlow(flowId, 120_000);
  console.log(JSON.stringify({ flowId, status: result.status, error: result.errorMessage, output: result.status === 'completed' ? result.singleOutput() : undefined }, null, 2));
} finally { await client.close(); blobCache.close(); }
