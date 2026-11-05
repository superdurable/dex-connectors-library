// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function projectWithTimeout(timeout: string): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'vercel', connectionName: 'vercel', provider: 'vercel', configuration: { timeout } }] },
    resolveCredential: async () => ({ access_token: 'test-accesstoken' }),
  } as unknown as LoadedProject;
}
it('reads the manifest timeout field from the project connection', () => {
  expect(() => newProjectConnection(projectWithTimeout('2s'), 'vercel')).not.toThrow();
  expect(() => newProjectConnection(projectWithTimeout('not-a-duration'), 'vercel')).toThrow(/timeout/);
});
