// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import type { CreatedPost, Post, PostPage } from '../../src/index.js';
import { ConnectionName, PostCreationFlow } from '../../examples/provider-operations/post-creation-flow.js';
import { PostLookupFlow } from '../../examples/provider-operations/post-lookup-flow.js';
import { PostListFlow } from '../../examples/provider-operations/post-list-flow.js';

it('runs all WordPress post operations through real Dex Workers and Clients', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-wordpress-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const createFlow = new PostCreationFlow(mock.connection());
  const lookupFlow = new PostLookupFlow(mock.connection());
  const listFlow = new PostListFlow(mock.connection());
  const registry = new Registry([createFlow, lookupFlow, listFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });

    const createId = `wordpress-typescript-create-${randomUUID()}`;
    await client.startFlow(createFlow, createId, { title: 'Meridian release', content: '<p>Draft.</p>', status: 'draft' });
    const created = await client.waitForFlow(createId, 60_000);
    expect(created.status, created.errorMessage).toBe('completed');
    expect(created.singleOutput<CreatedPost>()).toMatchObject({ id: 43, status: 'draft' });
    expect(mock.createPost().calls()).toHaveLength(1);

    const lookupId = `wordpress-typescript-lookup-${randomUUID()}`;
    await client.startFlow(lookupFlow, lookupId, { postId: 42 });
    const lookedUp = await client.waitForFlow(lookupId, 60_000);
    expect(lookedUp.status, lookedUp.errorMessage).toBe('completed');
    expect(lookedUp.singleOutput<Post>()).toMatchObject({ slug: 'meridian-release' });
    expect(mock.getPost().calls()).toHaveLength(1);

    const listId = `wordpress-typescript-list-${randomUUID()}`;
    await client.startFlow(listFlow, listId, { page: 1, perPage: 20 });
    const listed = await client.waitForFlow(listId, 60_000);
    expect(listed.status, listed.errorMessage).toBe('completed');
    expect(listed.singleOutput<PostPage>()).toMatchObject({ total: 1 });
    expect(mock.listPosts().calls()).toHaveLength(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
