// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { SendApplicationMessageInputSchema, SendGroupRobotMessageInputSchema, newProjectConnection } from '../../src/index.js';
import { ApplicationConnectionName, RobotConnectionName, WeComApplicationNotificationFlow, WeComRobotNotificationFlow } from './flow.js';

const project = await loadFromEnvironment();
const method = process.argv[2];
if (method !== 'robot' && method !== 'application') throw new TypeError('pass robot or application followed by JSON input');
const connection = newProjectConnection(project, method === 'robot' ? RobotConnectionName : ApplicationConnectionName);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-wecom-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([method === 'robot' ? new WeComRobotNotificationFlow(connection) : new WeComApplicationNotificationFlow(connection)]), blobCache,
  { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `wecom-${method}-${randomUUID()}`;
  const rawInput: unknown = JSON.parse(process.argv[3] || '{}');
  if (method === 'robot') {
    const flow = new WeComRobotNotificationFlow(connection);
    await client.startFlow(flow, flowId, SendGroupRobotMessageInputSchema.parse(rawInput));
  } else {
    const flow = new WeComApplicationNotificationFlow(connection);
    await client.startFlow(flow, flowId, SendApplicationMessageInputSchema.parse(rawInput));
  }
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
