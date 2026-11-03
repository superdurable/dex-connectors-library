// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import { ContentfulClient } from '../src/client.js';

const call = {
  id: 'contentful-test-call', connection: { provider: 'contentful', name: 'contentful' },
  operation: { connectorId: 'contentful', operationId: 'getEntry' },
  context: { flowId: 'contentful-test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as Call;

describe('Contentful client boundaries', () => {
  it('retries address refusals like other request errors', async () => {
    const client = new ContentfulClient({}, { resolve: async () => ({ deliveryToken: new SecretString('test-token') }) }, {
      send: async () => { throw new AddressRefusal('test refusal'); },
    });
    const result = await client.getEntry().invoke(call, { spaceId: 'space123', environmentId: 'master', entryId: 'entry-meridian' });
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('AVAILABILITY');
  });

  it('preserves provider numeric literals in both Query outputs', async () => {
    const credentials = { resolve: async () => ({ deliveryToken: new SecretString('test-token') }) };
    const entryJSON = '{"sys":{"id":"entry-meridian"},"fields":{"large":9007199254740993,"exponent":1e999}}';
    const pageJSON = '{"items":[{"large":9007199254740993,"exponent":1e999}],"total":1}';
    const client = new ContentfulClient({}, credentials, {
      send: async request => ({ status: 200, headers: {}, body: Buffer.from(request.url.includes('entry-meridian') ? entryJSON : pageJSON) }),
    });
    const entry = await client.getEntry().invoke(call, { spaceId: 'space123', environmentId: 'master', entryId: 'entry-meridian' });
    const page = await client.listEntries().invoke(call, { spaceId: 'space123', environmentId: 'master' });
    expect(entry.kind).toBe('branch');
    expect(page.kind).toBe('branch');
    if (entry.kind === 'branch') expect(JSON.stringify(entry.value)).toBe(`{"data":${entryJSON}}`);
    if (page.kind === 'branch') expect(JSON.stringify(page.value)).toBe(`{"data":${pageJSON}}`);
  });

  it('rejects malformed settings before making a request', () => {
    const credentials = { resolve: async () => ({ deliveryToken: new SecretString('test-token') }) };
    expect(() => new ContentfulClient({ baseUrl: 'https://user:secret@cdn.contentful.com' }, credentials)).toThrow();
    expect(() => new ContentfulClient({ maxResponseBytes: -1 }, credentials)).toThrow();
  });

  it('keeps invalid credentials terminal and secret safe', async () => {
    let sent = false;
    const client = new ContentfulClient({}, { resolve: async () => ({ deliveryToken: new SecretString('secret\r\nInjected: value') }) }, {
      send: async () => { sent = true; throw new Error('unexpected request'); },
    });
    const result = await client.getEntry().invoke(call, { spaceId: 'space123', environmentId: 'master', entryId: 'entry-meridian' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('defect');
      expect(result.failure?.kind).toBe('AUTHENTICATION');
      expect(result.failure?.message).not.toContain('secret');
    }
    expect(sent).toBe(false);
  });
});
