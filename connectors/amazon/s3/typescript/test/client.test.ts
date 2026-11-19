// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { S3Client } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';

const testCredentials = { accessKeyId: new SecretString('AKIAS3CONNECTORTEST1'), secretAccessKey: new SecretString('s3-connector-test/secret+value') };
const call = { id: '0c58ff90-76a5-5ab6-9099-092233c989ba', context: { cancellationSignal: new AbortController().signal } } as Call;

it('signs one request identically to the official Go SigV4 signer', async () => {
  let observed: ProviderRequest | undefined;
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, { resolve: async () => testCredentials }, {
    now: () => new Date('2026-10-25T09:00:00Z'),
    send: async request => { observed = request; return { status: 200, headers: {}, body: new Uint8Array() }; },
  });
  const session = await client.startSession(call);
  expect(session).toBeDefined();
  await client.exchange(call, session!, { method: 'GET', bucket: 'acme-reports', key: 'reports/weekly.md', query: { 'max-keys': '3' } });
  expect(observed?.url).toBe('http://127.0.0.1:12345/acme-reports/reports/weekly.md?max-keys=3');
  expect(observed?.headers?.authorization).toBe('AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/20261025/eu-west-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=b55faa0643e0e719465dc3cffa4f3052f1a394ae1580a13415befb1917f250fd');
  expect(JSON.stringify(observed)).not.toContain('s3-connector-test/secret+value');
});

it('escapes an object key once without changing its slash segments', async () => {
  let observedURL = '';
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, { resolve: async () => testCredentials }, {
    now: () => new Date('2026-10-25T09:00:00Z'),
    send: async request => { observedURL = request.url; return { status: 200, headers: {}, body: new Uint8Array() }; },
  });
  await client.exchange(call, { accessKeyId: 'AKIAS3CONNECTORTEST1', secretAccessKey: 's3-connector-test/secret+value', sessionToken: '' }, { method: 'GET', bucket: 'acme-reports', key: 'a b+c/é.txt' });
  expect(observedURL).toBe('http://127.0.0.1:12345/acme-reports/a%20b%2Bc/%C3%A9.txt');
});

it('signs PUT content length and metadata like the Go SigV4 signer', async () => {
  let authorization = '';
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, { resolve: async () => testCredentials }, {
    now: () => new Date('2026-10-25T09:00:00Z'),
    send: async request => { authorization = request.headers?.authorization ?? ''; return { status: 200, headers: {}, body: new Uint8Array() }; },
  });
  await client.exchange(call, { accessKeyId: 'AKIAS3CONNECTORTEST1', secretAccessKey: 's3-connector-test/secret+value', sessionToken: '' }, {
    method: 'PUT', bucket: 'acme-reports', key: 'reports/weekly.md', body: Buffer.from('hello\n'),
    headers: { 'content-type': 'text/plain', 'content-md5': 'sZRqySSS0jR8YjW00mERhA==', 'x-amz-meta-dex-idempotency-key': call.id, 'if-none-match': '*' },
  });
  expect(authorization).toBe('AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/20261025/eu-west-1/s3/aws4_request, SignedHeaders=content-length;content-md5;content-type;host;if-none-match;x-amz-content-sha256;x-amz-date;x-amz-meta-dex-idempotency-key, Signature=9f8d15f7a0330672fe67a13dae8fa52ee97b1b79652cb6a7c86eb70091b41b1e');
});

it('rejects invalid connection configuration before any provider request', () => {
  expect(() => new S3Client({ endpoint: 'http://example.com', region: 'eu-west-1' }, { resolve: async () => testCredentials })).toThrow();
  expect(() => new S3Client({ region: 'auto' }, { resolve: async () => testCredentials })).toThrow();
  expect(() => new S3Client({ endpoint: 'https://example.com', region: 'auto', defaultBucket: 'BAD' }, { resolve: async () => testCredentials })).toThrow();
});

it('suppresses malformed credentials without including them in errors', async () => {
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' }, {
    resolve: async () => ({ accessKeyId: new SecretString('access/key'), secretAccessKey: new SecretString('private') }),
  });
  expect(await client.startSession(call)).toBeUndefined();
});

it.each([
  ['reauthorization required', new ReauthorizationRequiredError(), true],
  ['connection object not found', new ObjectNotFoundError(), true],
  ['connection not selected', new ConnectionNotSelectedError(), true],
  ['credential broker rejected', new CredentialBrokerRejectedError(), true],
  ['malformed credential material', new CredentialMaterialError('credential access_key_id is required'), true],
  ['SDK provider decoding failure', new TypeError('project credential decoding failed'), true],
  ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
  ['static connection not configured', new Error('connection is not configured'), false],
  ['unlisted error', new TypeError('unlisted'), false],
])('classifies a %s credential failure without sending', async (_name, credentialFailure, isTerminal) => {
  let requests = 0;
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1', defaultBucket: 'acme-reports' }, { resolve: async () => { throw credentialFailure; } }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const expected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
  expect(await client.putObject().invoke(call, { key: 'report.txt', contentType: 'text/plain', textContent: 'hello' })).toMatchObject(expected);
  expect(await client.listObjects().invoke(call, {})).toMatchObject(expected);
  expect(requests).toBe(0);
});

it('keeps the released startSession result for a transient credential failure', async () => {
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1' },
    { resolve: async () => { throw new CredentialBrokerUnavailableError(); } });
  expect(await client.startSession(call)).toBeUndefined();
});

it('selects providerRejected for a putObject 301 PermanentRedirect without repeating', async () => {
  let requests = 0;
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1', defaultBucket: 'acme-reports' },
    { resolve: async () => testCredentials },
    { now: () => new Date('2026-10-25T09:00:00Z'), send: async () => {
      requests++;
      return { status: 301, headers: { 'x-amz-bucket-region': 'ap-southeast-2' }, body: new TextEncoder().encode('<Error><Code>PermanentRedirect</Code></Error>') };
    } });
  expect(await client.putObject().invoke(call, { key: 'report.txt', contentType: 'text/plain', textContent: 'hello' }))
    .toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'PROVIDER_REJECTION' } });
  expect(requests).toBe(1);
});

it.each([405, 409, 425, 451])('retries a putObject answered with unlisted HTTP %i', async status => {
  const client = new S3Client({ endpoint: 'http://127.0.0.1:12345', region: 'eu-west-1', defaultBucket: 'acme-reports' },
    { resolve: async () => testCredentials },
    { now: () => new Date('2026-10-25T09:00:00Z'), send: async () => ({ status, headers: {}, body: new Uint8Array() }) });
  expect(await client.putObject().invoke(call, { key: 'report.txt', contentType: 'text/plain', textContent: 'hello' })).toMatchObject({ kind: 'retry' });
});
