// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { HackerNewsClient, newConnection, type User } from '../../src/index.js';
import { ConnectionName } from './flow.js';
import { readOnce } from './query-integration.js';
import { ProfileFlow } from './profile-flow.js';

async function readProfile(provider: HackerNewsClient, username: string): Promise<User> {
  const flow = new ProfileFlow(newConnection(provider, { provider: 'hacker-news', name: ConnectionName }));
  return readOnce(flow, { username });
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
