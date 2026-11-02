// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ContentfulClient } from '../src/client.js';

const call = {
  id: 'contentful-test-call', connection: { provider: 'contentful', name: 'contentful' },
  operation: { connectorId: 'contentful', operationId: 'getEntry' },
  context: { flowId: 'contentful-test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as Call;

describe('Contentful client boundaries', () => {
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
