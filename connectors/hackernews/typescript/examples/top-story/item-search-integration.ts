// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { HackerNewsClient, newConnection, type ItemSearchPage } from '../../src/index.js';
import { ItemSearchFlow } from './item-search-flow.js';
import { ConnectionName } from './flow.js';
import { readOnce } from './query-integration.js';

function search(provider: HackerNewsClient, query: string): Promise<ItemSearchPage> {
  const flow = new ItemSearchFlow(newConnection(provider, { provider: 'hacker-news', name: ConnectionName }));
  return readOnce(flow, { query });
}

async function verifyRetry(): Promise<void> {
  let requests = 0;
  const provider = new HackerNewsClient({}, { send: async request => {
    assert.equal(request.url, 'https://hn.algolia.com/api/v1/search?hitsPerPage=2&page=0&query=~+%2A');
    requests++;
    const body = { page: 0, nbPages: 2, nbHits: 3, hits: [{ objectID: '42', created_at_i: 1, _tags: ['comment'], comment_text: '<p>A &amp; B</p>' }] };
    return { status: requests === 1 ? 503 : 200, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  } });
  const page = await search(provider, '~ *');
  assert.deepEqual(page.items?.map(item => [item.id, item.text]), [[42, 'A & B']]);
  assert.equal(page.nextPage, 1);
  assert.equal(requests, 2);
}

if (process.argv.length === 2) await verifyRetry();
else if (process.argv.length === 3) {
  const page = await search(new HackerNewsClient({}), process.argv[2]!);
  console.log(page.items?.map(item => item.id), page.nextPage);
} else throw new Error('usage: npm run integration:item-search -- [keywords]');
