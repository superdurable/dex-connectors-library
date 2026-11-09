// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { MongoClient } from 'mongodb';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { MongoDBClient } from '../src/client.js';

it('reads BSON through the real MongoDB wire protocol and detects one extra match', async () => {
  const uri = process.env.MONGODB_CONNECTOR_TEST_URI;
  if (!uri) throw new Error('MONGODB_CONNECTOR_TEST_URI is required for the native-driver CI job');
  const collectionName = `connector_ts_${randomUUID().replaceAll('-', '')}`;
  const provider = new MongoClient(uri);
  await provider.connect();
  const collection = provider.db('connector_test').collection(collectionName);
  try {
    await collection.insertMany([{ status: 'active', count: 3 }, { status: 'active', count: 4 }]);
    const connector = new MongoDBClient({ database: 'connector_test', maxDocuments: 1 }, {
      resolve: async () => ({ connectionUri: new SecretString(uri) }),
    });
    const call = { id: randomUUID(), context: {} } as Call;
    const result = await connector.findDocuments().invoke(call, { collection: collectionName, filter: { status: 'active' } });
    expect(result).toMatchObject({ kind: 'branch', branch: 'truncated' });
    if (result.kind !== 'branch') throw new Error('MongoDB read unexpectedly retried');
    expect(result.value?.documents).toHaveLength(1);
    expect(result.value?.documents?.[0]?.count).toEqual(expect.objectContaining({ $numberInt: expect.stringMatching(/^[34]$/) }));
    const empty = await connector.findDocuments().invoke(call, { collection: collectionName, filter: { status: 'missing' } });
    expect(empty).toMatchObject({ kind: 'branch', branch: 'found', value: { documents: [] } });
  } finally { await collection.drop(); await provider.close(); }
}, 60_000);
