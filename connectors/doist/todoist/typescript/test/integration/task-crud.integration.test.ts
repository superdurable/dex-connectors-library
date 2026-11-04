// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { TodoistClient } from '../../src/client.js';
import { newConnection } from '../../src/generated/connection.js';
import { TaskLifecycleFlow, type TaskLifecycleOutcome } from '../../examples/task-crud/flow/workflow.js';

it('runs all five Todoist operations through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const taskID = '6XGgmFVcrG5RRjVr';
  const projectID = '6XGgm6PHrGgMpCFX';
  let currentTask: Record<string, unknown> | undefined;
  const methods: string[] = [];
  const requestIDs: string[] = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const payload = chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) as Record<string, unknown> : {};
    const url = new URL(request.url ?? '/', 'http://127.0.0.1');
    methods.push(`${request.method} ${url.pathname}`);
    if (request.headers.authorization !== 'Bearer test-todoist-secret') { response.writeHead(401).end(); return; }
    if (request.method === 'POST' && url.pathname === '/api/v1/tasks') {
      if (payload.content === 'reject') { response.writeHead(403).end(); return; }
      requestIDs.push(String(request.headers['x-request-id'] ?? ''));
      currentTask = { id: taskID, project_id: projectID, content: payload.content, description: payload.description ?? '', checked: false };
      response.writeHead(201, { 'Content-Type': 'application/json' }).end(JSON.stringify(currentTask));
      return;
    }
    if (request.method === 'GET' && url.pathname === `/api/v1/tasks/${taskID}`) {
      if (!currentTask) { response.writeHead(404).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(currentTask));
      return;
    }
    if (request.method === 'POST' && url.pathname === `/api/v1/tasks/${taskID}`) {
      if (!currentTask) { response.writeHead(404).end(); return; }
      currentTask = { ...currentTask, ...payload };
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(currentTask));
      return;
    }
    if (request.method === 'GET' && url.pathname === '/api/v1/tasks' && url.searchParams.get('project_id') === projectID) {
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ results: currentTask ? [currentTask] : [], next_cursor: null }));
      return;
    }
    if (request.method === 'DELETE' && url.pathname === `/api/v1/tasks/${taskID}`) {
      currentTask = undefined;
      response.writeHead(204).end();
      return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Todoist fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-todoist-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new TodoistClient({ baseUrl: `http://127.0.0.1:${address.port}/api/v1` }, {
    resolve: async () => ({ apiToken: new SecretString('test-todoist-secret') }),
  });
  const flow = new TaskLifecycleFlow(newConnection(provider, { provider: 'todoist', name: 'todoist' }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successfulID = `todoist-ts-success-${randomUUID()}`;
    await client.startFlow(flow, successfulID, { content: 'Original', description: 'Details', updateContent: 'Updated', updateDescription: '' });
    const success = await client.waitForFlow(successfulID, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    const outcome = success.singleOutput<TaskLifecycleOutcome>();
    expect(outcome.created.id).toBe(taskID);
    expect(outcome.read.content).toBe('Original');
    expect(outcome.updated.content).toBe('Updated');
    expect(outcome.updated.description).toBe('');
    expect(outcome.page.tasks?.map(task => task.id)).toContain(taskID);
    expect(outcome.deletion.taskId).toBe(taskID);
    expect(methods).toEqual(['POST /api/v1/tasks', `GET /api/v1/tasks/${taskID}`, `POST /api/v1/tasks/${taskID}`, 'GET /api/v1/tasks', `DELETE /api/v1/tasks/${taskID}`]);
    expect(requestIDs[0]).toMatch(/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/);
    const rejectedID = `todoist-ts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedID, { content: 'reject', updateContent: 'Unused' });
    const rejection = await client.waitForFlow(rejectedID, 60_000);
    expect(rejection.status).toBe('failed');
    expect(rejection.errorMessage).toContain('providerRejected');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
