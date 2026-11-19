// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { DadaClient } from '../src/index.js';

it('audits all visible connection fields and protects the app secret', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string; configurationUI?: unknown }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://newopen.imdada.cn');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('source_id');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['appKey', 'appSecret', 'sourceID']);
  expect(manifest.spec.auth.fields.map(field => field.type)).toEqual(['string', 'secretString', 'string']);
  for (const field of manifest.spec.auth.fields) { expect(field.description).toContain('https://newopen.imdada.cn'); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['queryOrderStatus']);
  expect(manifest.spec.operations[0]?.configurationUI).toBeUndefined();
  const credentials = { appKey: '123456', appSecret: new SecretString('private-secret'), sourceID: '73753' };
  const client = new DadaClient({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain(credentials.appSecret.reveal());
  expect(() => new DadaClient({ baseUrl: 'https://newopen.imdada.cn/api/order/status/query' }, { resolve: async () => credentials })).toThrow();
  expect(() => new DadaClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
