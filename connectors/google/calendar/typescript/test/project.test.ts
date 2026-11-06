// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'google-calendar', connectionName: 'google-calendar', provider: 'google', configuration }] },
    connections: { scope: { projectId: 'calendar-test', kind: 'live' } },
  } as unknown as LoadedProject;
}

describe('Calendar project configuration', () => {
  it('keeps the project timeout and rejects malformed or unknown fields', () => {
    expect(() => newProjectConnection(project({ timeout: true }), 'google-calendar')).toThrow();
    expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'google-calendar')).toThrow();
    expect(() => newProjectConnection(project({ extra: 'ignored' }), 'google-calendar')).toThrow();
    const connection = newProjectConnection(project({ timeout: '2s' }), 'google-calendar');
    expect((connection.operations as unknown as { timeoutMs: number }).timeoutMs).toBe(2_000);
  });
});
