// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it, vi } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Call } from '@superdurable/connectors-sdk';
import { newProjectConnection } from '../src/project.js';

const connection = { provider: 'webflow', name: 'webflow' };
const call = { id: 'project-call', connection, operation: { connectorId: 'webflow', operationId: 'listCollectionItems' }, context: { cancellationSignal: new AbortController().signal } } as Call;

function projectWithConfiguration(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'webflow', connectionName: 'webflow', provider: 'webflow', configuration }] },
    resolveCredential: async () => ({ site_token: 'test-token' }),
  } as unknown as LoadedProject;
}

it('converts a numeric Go duration from nanoseconds to whole milliseconds', async () => {
  const timeoutSpy = vi.spyOn(AbortSignal, 'timeout');
  try {
    const configured = newProjectConnection(projectWithConfiguration({ baseUrl: 'http://127.0.0.1:32199/v2', timeout: 2_500_000 }), 'webflow');
    await configured.operations.listCollectionItems().invoke(call, { collectionId: '62b720ef280c7a7a3be8cabe' });
    expect(timeoutSpy).toHaveBeenCalledWith(3);
  } finally { timeoutSpy.mockRestore(); }
});

it('rejects unknown and invalid project fields', () => {
  expect(() => newProjectConnection(projectWithConfiguration({ timeout: true }), 'webflow')).toThrow();
  expect(() => newProjectConnection(projectWithConfiguration({ unknownField: 'value' }), 'webflow')).toThrow();
});
