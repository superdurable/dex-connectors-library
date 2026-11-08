// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, TaskListsInspectionFlow } from './lists-flow.js';
import { TasksInspectionFlow } from './tasks-flow.js';

const mode = process.argv[2];
const listId = process.argv[3];
if (mode !== 'lists' && (mode !== 'tasks' || !listId)) throw new TypeError('usage: npm run example:start -- lists | tasks <list ID>');
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const listFlow = new TaskListsInspectionFlow(connection);
const taskFlow = new TasksInspectionFlow(connection);
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-microsoft-todo-ts-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([listFlow, taskFlow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `microsoft-todo-${mode}-${randomUUID()}`;
  if (mode === 'lists') await client.startFlow(listFlow, flowId, {});
  else await client.startFlow(taskFlow, flowId, { listId: listId ?? '' });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
