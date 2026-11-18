// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { Client } from '../src/index.js';

const call = { id: 'algolia-test-call', context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Algolia client configuration and credential safety', () => {
  it('rejects invalid local settings at construction', () => {
    const credentials = { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('safe-key') }) };
    expect(() => new Client({ baseUrl: 'http://example.com/1' }, credentials)).toThrow();
    expect(() => new Client({ timeoutMs: -1 }, credentials)).toThrow();
    expect(() => new Client({ maxResponseBytes: -1 }, credentials)).toThrow();
  });

  it('uses the application-specific public host and keeps the key in a header', async () => {
    let observedUrl = '';
    let observedKey = '';
    const client = new Client({}, { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('safe-key') }) }, async request => {
      observedUrl = request.url;
      observedKey = request.headers?.['x-algolia-api-key'] ?? '';
      return { status: 200, headers: {}, body: Buffer.from('{"nbHits":0,"hits":[]}') };
    });
    const result = await client.search().invoke(call, { indexName: 'products', query: '' });
    expect(result.kind).toBe('branch');
    expect(observedUrl).toBe('https://TESTAPP.algolia.net/1/indexes/products/query');
    expect(observedUrl).not.toContain('safe-key');
    expect(observedKey).toBe('safe-key');
  });

  it.each([
    ['reauthorization required', new ReauthorizationRequiredError(), true],
    ['connection object not found', new ObjectNotFoundError(), true],
    ['connection not selected', new ConnectionNotSelectedError(), true],
    ['credential broker rejected', new CredentialBrokerRejectedError(), true],
    ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
    ['static connection not configured', new Error('connection is not configured'), false],
    ['unlisted error', new TypeError('unlisted'), false],
  ])('classifies a %s credential failure without sending', async (_name, credentialFailure, isTerminal) => {
    let requests = 0;
    const client = new Client({}, { resolve: async () => { throw credentialFailure; } }, async () => { requests++; throw new Error('unexpected send'); });
    const expected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
    expect(await client.saveObject().invoke(call, { indexName: 'products', objectID: 'product-1', attributes: { name: 'Meridian' } })).toMatchObject(expected);
    expect(await client.search().invoke(call, { indexName: 'products', query: '' })).toMatchObject(expected);
    expect(requests).toBe(0);
  });

  it('does not dispatch when credentials contain a line break', async () => {
    let wasSent = false;
    const client = new Client({}, { resolve: async () => ({ applicationId: 'TESTAPP', apiKey: new SecretString('unsafe\nkey') }) }, async () => {
      wasSent = true;
      throw new Error('unexpected send');
    });
    const result = await client.search().invoke(call, { indexName: 'products', query: '' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(wasSent).toBe(false);
  });
});
