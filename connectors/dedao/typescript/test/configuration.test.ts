// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { DedaoClient } from '../src/index.js';

it('audits connection guidance and keeps the Key and Client ID private', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string; configurationUI?: unknown }> } };
  expect(manifest.spec.auth.guide.startURL).toContain('doc.biji.com');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('data.tasks[0].task_id');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['apiKey', 'clientID']);
  for (const field of manifest.spec.auth.fields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://www.biji.com/openapi'); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['getNoteTaskStatus']);
  expect(manifest.spec.operations[0]?.configurationUI).toBeUndefined();
  const credentials = { apiKey: new SecretString('gk_live_private'), clientID: new SecretString('cli_private') };
  const client = new DedaoClient({}, { resolve: async () => credentials });
  for (const value of Object.values(credentials)) expect(JSON.stringify(client)).not.toContain(value.reveal());
  expect(() => new DedaoClient({ maxResponseBytes: 16 * 1024 * 1024 + 1 }, { resolve: async () => credentials })).toThrow();
  expect(() => new DedaoClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
