// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { EntraClient } from '../src/client.js';
import { newProjectConnection } from '../src/project.js';

it('uses Go configuration defaults when fields are zero or blank', () => {
  const client = new EntraClient({ maxResponseBytes: 0, listUsersPageSize: 0, creationKeyAttribute: '' },
    { resolve: async () => { throw new Error('unused'); }, resolveWithRefresh: async () => { throw new Error('unused'); } });
  expect(client.maxResponseBytes).toBe(4_194_304);
  expect(client.listUsersPageSize).toBe(100);
  expect(client.creationKeyAttribute).toBe('extensionAttribute15');
});

it.each([{ listUsersPageSize: '50' }, { creationKeyAttribute: true }])('rejects a wrong-typed project setting', setting => {
  const project = { configuration: { connections: [{ connectorId: 'microsoft-entra-id', connectionName: 'directory',
    provider: 'microsoft', configuration: setting }] } } as unknown as LoadedProject;
  expect(() => newProjectConnection(project, 'directory')).toThrow();
});
