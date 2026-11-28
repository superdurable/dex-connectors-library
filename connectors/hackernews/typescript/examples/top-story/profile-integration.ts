// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { HackerNewsClient, newConnection, type User } from '../../src/index.js';
import { ConnectionName } from './flow.js';
import { ProfileFlow } from './profile-flow.js';

async function readProfile(provider: HackerNewsClient, username: string): Promise<User> {
  const flow = new ProfileFlow(newConnection(provider, { provider: 'hacker-news', name: ConnectionName }));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-hackernews-profile-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 24 });
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 15_000 });
    const id = `hackernews-profile-typescript-${randomUUID()}`;
    await client.startFlow(flow, id, { username });
    const result = await client.waitForFlow(id, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    return result.singleOutput<User>();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

async function verifyRetry(): Promise<void> {
  let requests = 0;
  const provider = new HackerNewsClient({}, { send: async request => {
    assert.equal(request.url, 'https://hacker-news.firebaseio.com/v0/user/a%20b+me.json');
    requests++;
    return { status: requests === 1 ? 503 : 200, headers: {}, body: Buffer.from('{"id":"a b+me","karma":12,"profileUrl":"spoofed"}') };
  } });
  const result = await readProfile(provider, 'a b+me');
  assert.deepEqual(result, { id: 'a b+me', created: 0, karma: 12, profileUrl: 'https://news.ycombinator.com/user?id=a+b%2Bme' });
  assert.equal(requests, 2);
}

if (process.argv.length === 2) await verifyRetry();
else if (process.argv.length === 3) console.log((await readProfile(new HackerNewsClient({}), process.argv[2]!)).profileUrl);
else throw new Error('usage: npm run integration:profile -- [public-username]');
