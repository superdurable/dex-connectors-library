// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Int32, ObjectId } from 'bson';
import { MongoClient, MongoNetworkError, MongoServerError } from 'mongodb';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { MongoDBClient } from '../src/client.js';

const call = { id: 'mongodb-ts-test', context: {} } as Call;

function fixture(documents: Array<Record<string, unknown>> | Error, maxDocuments = 100, maxResponseBytes = 1_048_576) {
  let requestedLimit = 0;
  let submittedFilter: unknown;
  let requests = 0;
  const driver = {
    async connect() { return this; },
    async close() {},
    db() { return { collection() { return { find(filter: unknown) {
      requests++;
      submittedFilter = filter;
      return { limit(count: number) { requestedLimit = count; return {
        async *[Symbol.asyncIterator]() {
          if (documents instanceof Error) throw documents;
          for (const document of documents.slice(0, count)) yield document;
        },
        async close() {},
      }; } };
    } }; } }; },
  } as unknown as MongoClient;
  const client = new MongoDBClient({ database: 'example', maxDocuments, maxResponseBytes }, {
    resolve: async () => ({ connectionUri: new SecretString('mongodb://reader:secret@localhost:27017') }),
  }, { createDriver: () => driver });
  return { client, metrics: () => ({ requestedLimit, submittedFilter, requests }) };
}

it('preserves canonical Extended JSON and fetches one extra document', async () => {
  const { client, metrics } = fixture([{ _id: new ObjectId('507f1f77bcf86cd799439011'), count: new Int32(3) }]);
  const result = await client.findDocuments().invoke(call, { collection: 'orders', filter: { status: 'active' } });
  expect(result).toMatchObject({ kind: 'branch', branch: 'found', value: {
    documents: [{ _id: { $oid: '507f1f77bcf86cd799439011' }, count: { $numberInt: '3' } }],
  } });
  expect(metrics()).toMatchObject({ requestedLimit: 101, submittedFilter: { status: 'active' }, requests: 1 });
});

it('truncates by document count and rejects JavaScript before provider I/O', async () => {
  const { client, metrics } = fixture([{ status: 'first' }, { status: 'second' }], 1);
  const result = await client.findDocuments().invoke(call, { collection: 'orders', filter: {} });
  expect(result).toMatchObject({ kind: 'branch', branch: 'truncated', value: { documents: [{ status: 'first' }] } });
  const invalid = await client.findDocuments().invoke(call, { collection: 'orders', filter: { $where: 'return true' } });
  expect(invalid).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(metrics().requests).toBe(1);
});

it('classifies response byte limits, command rejection, and network retry', async () => {
  const small = fixture([{ status: 'active' }], 100, 8);
  expect(await small.client.findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
    kind: 'branch', branch: 'truncated', value: { documents: [] },
  });
  const rejected = fixture(new MongoServerError({ ok: 0, code: 13, errmsg: 'unauthorized' }));
  expect(await rejected.client.findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
    kind: 'branch', branch: 'providerRejected',
  });
  const unavailable = fixture(new MongoNetworkError('network lost'));
  expect(await unavailable.client.findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
    kind: 'retry', failure: { kind: 'TRANSPORT' },
  });
  const retryable = fixture(new MongoServerError({
    ok: 0, code: 91, errmsg: 'not primary', errorLabels: ['RetryableReadError'],
  }));
  expect(await retryable.client.findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
    kind: 'retry', failure: { kind: 'AVAILABILITY' },
  });
});
