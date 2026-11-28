// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { HackerNewsClient, newConnection, type ArticleThread } from '../../src/index.js';
import { ArticleThreadFlow } from './article-flow.js';
import { ConnectionName } from './flow.js';
import { readOnce } from './query-integration.js';

function readArticle(provider: HackerNewsClient, articleId: number): Promise<ArticleThread> {
  const flow = new ArticleThreadFlow(newConnection(provider, { provider: 'hacker-news', name: ConnectionName }));
  return readOnce(flow, { articleId });
}

async function verifyRetry(): Promise<void> {
  let requests = 0;
  const provider = new HackerNewsClient({}, { send: async request => {
    assert.equal(request.url, 'https://hn.algolia.com/api/v1/items/42');
    requests++;
    return { status: requests === 1 ? 503 : 200, headers: {}, body: Buffer.from('{"id":42,"title":"Article","children":[{"id":7,"children":[{"id":9}]}]}') };
  } });
  const article = await readArticle(provider, 42);
  assert.equal(article.comments?.[0]?.replies?.[0]?.discussionUrl, 'https://news.ycombinator.com/item?id=9');
  assert.equal(requests, 2);
}

if (process.argv.length === 2) await verifyRetry();
else if (process.argv.length === 3) {
  const article = await readArticle(new HackerNewsClient({}), Number(process.argv[2]));
  console.log(article.discussionUrl, article.comments?.length);
} else throw new Error('usage: npm run integration:article -- [article-id]');
