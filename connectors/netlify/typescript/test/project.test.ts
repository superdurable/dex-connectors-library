// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'netlify', connectionName: 'netlify', provider: 'netlify', configuration }] },
  } as unknown as LoadedProject;
}

it('converts Dex Web timeout durations to the client request timeout', () => {
  expect(Reflect.get(newProjectConnection(project({ timeout: '60s' }), 'netlify').operations, 'timeoutMs')).toBe(60_000);
  expect(Reflect.get(newProjectConnection(project({ timeout: 60_000_000_000 }), 'netlify').operations, 'timeoutMs')).toBe(60_000);
});

it('rejects malformed timeout values instead of silently dropping them', () => {
  expect(() => newProjectConnection(project({ timeout: 'bogus' }), 'netlify')).toThrow('positive Go duration');
  expect(() => newProjectConnection(project({ timeout: true }), 'netlify')).toThrow();
});
