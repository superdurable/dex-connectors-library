// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { MicrosoftTodoClient } from '../../src/client.js';
import { newConnection, type Credentials, type TaskListsPage, type TasksPage } from '../../src/index.js';
import { ConnectionName, TaskListsInspectionFlow } from '../../examples/task-inspection/lists-flow.js';
import { TasksInspectionFlow } from '../../examples/task-inspection/tasks-flow.js';

it('runs both To Do Queries and retries one 503 through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const requests: string[] = [];
  const credentials: Credentials = {
    oauthClientId: 'sample-client-id', oauthClientSecret: new SecretString('sample-client-secret'),
    accessToken: new SecretString('sample-access-token'), refreshToken: new SecretString('sample-refresh-token'),
  };
  const provider = new MicrosoftTodoClient({}, new StaticCredentialProvider([[{ provider: 'microsoft', name: ConnectionName }, credentials]]), {
    send: async request => {
      expect(request.headers?.Authorization).toBe('Bearer sample-access-token');
      const url = new URL(request.url);
      requests.push(url.pathname);
      if (url.pathname === '/v1.0/me/todo/lists')
        return { status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify({ value: [{ id: 'sample-list-1', displayName: 'Tasks' }] })) };
      expect(url.pathname).toBe('/v1.0/me/todo/lists/sample-list-1/tasks');
      if (requests.filter(path => path.endsWith('/tasks')).length === 1) return { status: 503, headers: {}, body: new Uint8Array() };
      return { status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify({ value: [{ id: 'sample-task-1', title: 'Review request', status: 'notStarted' }] })) };
    },
  });
  const connection = newConnection(provider, { provider: 'microsoft', name: ConnectionName });
  const listFlow = new TaskListsInspectionFlow(connection);
  const taskFlow = new TasksInspectionFlow(connection);
  const registry = new Registry([listFlow, taskFlow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-microsoft-todo-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const listId = `todo-ts-lists-${randomUUID()}`;
    await client.startFlow(listFlow, listId, {});
    const lists = await client.waitForFlow(listId, 60_000);
    expect(lists.status, lists.errorMessage).toBe('completed');
    expect(lists.singleOutput<TaskListsPage>().lists).toEqual([{ id: 'sample-list-1', displayName: 'Tasks' }]);
    const taskId = `todo-ts-tasks-${randomUUID()}`;
    await client.startFlow(taskFlow, taskId, { listId: 'sample-list-1' });
    const tasks = await client.waitForFlow(taskId, 60_000);
    expect(tasks.status, tasks.errorMessage).toBe('completed');
    expect(tasks.singleOutput<TasksPage>().tasks).toEqual([{ id: 'sample-task-1', title: 'Review request', status: 'notStarted' }]);
    expect(requests).toHaveLength(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
