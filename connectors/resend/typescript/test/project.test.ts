// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'resend', connectionName: 'resend', provider: 'resend', configuration }] },
    resolveCredential: async () => ({ api_key: 'test-secret-token' }),
  } as unknown as LoadedProject;
}

describe('Resend project connection', () => {
  it.each([{ timeout: '30s' }, { timeout: 30_000_000_000 }])('uses the manifest timeout $timeout for provider requests', async configuration => {
    let requestTimeout = 0;
    const connection = newProjectConnection(project(configuration), 'resend', { send: async request => {
      requestTimeout = request.timeoutMs ?? 0;
      return { status: 200, headers: {}, body: Buffer.from('{"id":"mail"}') };
    } });
    const call = { id: 'project-test', idempotencyKey: 'project-test', context: {} } as Call;
    await connection.operations.sendEmail().invoke(call, {
      from: 'sender@example.test', to: ['recipient@example.test'], subject: 'Welcome', html: '<p>Hello</p>',
    });
    expect(requestTimeout).toBe(30_000);
  });

  it('rejects unknown or malformed project settings', () => {
    expect(() => newProjectConnection(project({ timeoutMs: 30_000 }), 'resend')).toThrow();
    expect(() => newProjectConnection(project({ timeout: 'invalid' }), 'resend')).toThrow();
    expect(() => newProjectConnection(project({ timeout: 0 }), 'resend')).toThrow();
  });
});
