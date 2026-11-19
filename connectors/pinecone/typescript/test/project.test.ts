// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'pinecone', connectionName: 'pinecone', provider: 'pinecone', configuration }] },
    resolveCredential: async () => ({ api_key: 'safe-test-key' }),
  } as unknown as LoadedProject;
}

describe('Pinecone project connection', () => {
  it('rejects unknown and malformed timeout fields', () => {
    expect(() => newProjectConnection(project({ baseUrl: 'https://example.com', timeout: 'bogus' }), 'pinecone')).toThrow();
    expect(() => newProjectConnection(project({ baseUrl: 'https://example.com', timeoutMs: 45000 }), 'pinecone')).toThrow();
    expect(() => newProjectConnection(project({ baseUrl: 'https://example.com', unknown: true }), 'pinecone')).toThrow();
  });

  it.each([{ timeout: '45s' }, { timeout: 45_000_000_000 }])('applies the project timeout $timeout', async configuration => {
    let requestTimeout = 0;
    const connection = newProjectConnection(project({ baseUrl: 'https://example.com', ...configuration }), 'pinecone', { send: async request => {
      requestTimeout = request.timeoutMs ?? 0;
      return { status: 200, headers: {}, body: Buffer.from('{"matches":[]}') };
    } });
    const call = { id: 'project-test', context: { cancellationSignal: new AbortController().signal } } as Call;
    await connection.operations.queryVectors().invoke(call, { vector: [1], topK: 1 });
    expect(requestTimeout).toBe(45_000);
  });
});
