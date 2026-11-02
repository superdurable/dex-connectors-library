// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function projectWithTimeout(timeout: string | number): LoadedProject {
  return {
    configuration: {
      connections: [{
        connectorId: 'coda', connectionName: 'coda', provider: 'coda',
        configuration: { baseUrl: 'https://coda.io/apis/v1', timeout, maxResponseBytes: 2_097_152 },
      }],
    },
    resolveCredential: async () => ({ api_token: 'inert-token' }),
  } as unknown as LoadedProject;
}

it('accepts a Dex Web Go duration and rejects an invalid timeout', () => {
  expect(newProjectConnection(projectWithTimeout('2s'), 'coda').reference.name).toBe('coda');
  expect(newProjectConnection(projectWithTimeout(2_000_000_000), 'coda').reference.name).toBe('coda');
  expect(() => newProjectConnection(projectWithTimeout('2 seconds'), 'coda')).toThrow(/positive Go duration/);
});
