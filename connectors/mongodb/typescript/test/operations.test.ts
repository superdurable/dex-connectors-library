// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import dns from 'node:dns';
import { Int32, ObjectId } from 'bson';
import { MongoClient, MongoNetworkError, MongoServerError } from 'mongodb';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { afterEach, expect, it, vi } from 'vitest';
import { MongoDBClient } from '../src/client.js';

const call = { id: 'mongodb-ts-test', context: {} } as Call;

afterEach(() => { vi.restoreAllMocks(); });

function nativeDriverClient(connectionUri: string) {
  return new MongoDBClient({ database: 'example' }, {
    resolve: async () => ({ connectionUri: new SecretString(connectionUri) }),
  });
}

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

it.each(['ETIMEOUT', 'ESERVFAIL', 'ENOTFOUND'])('retries mongodb+srv:// DNS lookup failure %s', async (code) => {
  const resolve = vi.spyOn(dns.promises, 'resolve').mockImplementation(async (hostname: string, recordType?: string) => {
    throw Object.assign(new Error(`${code} ${hostname}`), { code, syscall: recordType === 'SRV' ? 'querySrv' : 'queryTxt', hostname });
  });
  const client = nativeDriverClient('mongodb+srv://reader:secret@cluster.example.com/example');
  expect(await client.findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
    kind: 'retry', failure: { kind: 'TRANSPORT', message: 'MongoDB DNS lookup failed' },
  });
  expect(resolve).toHaveBeenCalledWith('_mongodb._tcp.cluster.example.com', 'SRV');
});

it('selects defect for connection URIs the native driver rejects before I/O', async () => {
  for (const connectionUri of ['mongodb://reader:secret@localhost:notaport', 'mongodb+srv://reader:secret@cluster.example.com/?directConnection=true']) {
    expect(await nativeDriverClient(connectionUri).findDocuments().invoke(call, { collection: 'orders', filter: {} })).toMatchObject({
      kind: 'branch', branch: 'defect', failure: { message: 'MongoDB connection URI was rejected' },
    });
  }
});
