// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { DieyuyunClient } from '../src/index.js';

it('audits connection guidance and keeps the API Key private', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string; configurationUI?: unknown }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://open-console.dieyuyun.com/');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('Key Management');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['api_key']);
  expect(manifest.spec.auth.fields[0]?.type).toBe('secretString');
  expect(manifest.spec.auth.fields[0]?.description).toContain('https://open-console.dieyuyun.com/');
  expect(manifest.spec.auth.fields[0]?.description).toMatch(/blank/i);
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['getAccountBalance']);
  expect(manifest.spec.operations[0]?.configurationUI).toBeUndefined();
  const credentials = { apiKey: new SecretString('private-key') };
  const client = new DieyuyunClient({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain(credentials.apiKey.reveal());
  expect(() => new DieyuyunClient({ baseUrl: 'https://open.dieyuyun.com/v1/balance' }, { resolve: async () => credentials })).toThrow();
  expect(() => new DieyuyunClient({ maxResponseBytes: 0 }, { resolve: async () => credentials })).toThrow();
});
