// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { WeComClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ApplicationConnectionName, RobotConnectionName, WeComApplicationNotificationFlow, WeComRobotNotificationFlow } from '../../examples/basic/flow.js';

it('runs both fenced Mutations through a real Dex Worker and persists their results', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let robotRequests = 0;
  let tokenRequests = 0;
  let applicationRequests = 0;
  const server = createServer((request, response) => {
    if (request.url?.startsWith('/cgi-bin/webhook/send')) {
      robotRequests++;
      response.writeHead(robotRequests === 1 ? 503 : 200, { 'Content-Type': 'application/json' }).end(robotRequests === 1 ? '{}' : '{"errcode":0,"errmsg":"ok"}');
      return;
    }
    if (request.url?.startsWith('/cgi-bin/gettoken')) {
      tokenRequests++;
      response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"errcode":0,"access_token":"SYNTHETIC_ACCESS_TOKEN","expires_in":7200}');
      return;
    }
    if (request.url?.startsWith('/cgi-bin/message/send')) {
      applicationRequests++;
      response.writeHead(200, { 'Content-Type': 'application/json' }).end('{"errcode":0,"msgid":"SYNTHETIC-MSG-1","invaliduser":"","invalidparty":"","invalidtag":"","unlicenseduser":""}');
      return;
    }
    response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('WeCom fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-wecom-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials = new StaticCredentialProvider([
    [{ provider: 'wecom', name: RobotConnectionName }, { authMethodId: 'group-robot', webhookKey: new SecretString('SYNTHETIC-ROBOT-KEY') }],
    [{ provider: 'wecom', name: ApplicationConnectionName }, { authMethodId: 'application', corpId: 'ww1234567890abcdef', agentId: '1000002', corpSecret: new SecretString('SYNTHETIC_CORP_SECRET') }],
  ]);
  const provider = new WeComClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send: async request => {
    const target = new URL(request.url);
    const reply = await fetch(`http://127.0.0.1:${address.port}${target.pathname}${target.search}`, { method: request.method, headers: request.headers, body: request.body ? Buffer.from(request.body) : undefined, signal: request.signal });
    return { status: reply.status, headers: Object.fromEntries(reply.headers), body: new Uint8Array(await reply.arrayBuffer()) };
  } });
  const robot = new WeComRobotNotificationFlow(newConnection(provider, { provider: 'wecom', name: RobotConnectionName }));
  const application = new WeComApplicationNotificationFlow(newConnection(provider, { provider: 'wecom', name: ApplicationConnectionName }));
  const registry = new Registry([robot, application]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const robotFlowId = `wecom-robot-ts-${randomUUID()}`;
    await client.startFlow(robot, robotFlowId, { messageType: 'text', content: 'Synthetic deployment accepted' });
    const robotResult = await client.waitForFlow(robotFlowId, 60_000);
    expect(robotResult.status, robotResult.errorMessage).toBe('completed');
    expect(robotResult.singleOutput()).toEqual({ messageType: 'text', contentBytes: Buffer.byteLength('Synthetic deployment accepted') });
    expect(robotRequests).toBe(2);
    const applicationFlowId = `wecom-application-ts-${randomUUID()}`;
    await client.startFlow(application, applicationFlowId, { messageType: 'text', content: 'Synthetic incident INC-7', toUserIds: ['zhangsan'] });
    const applicationResult = await client.waitForFlow(applicationFlowId, 60_000);
    expect(applicationResult.status, applicationResult.errorMessage).toBe('completed');
    expect(applicationResult.singleOutput()).toEqual({ messageId: 'SYNTHETIC-MSG-1', invalidUserIds: [], invalidDepartmentIds: [], invalidTagIds: [], unlicensedUserIds: [] });
    expect(tokenRequests).toBe(1);
    expect(applicationRequests).toBe(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
