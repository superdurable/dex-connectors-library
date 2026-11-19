// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { ChuanglanClient } from '../src/index.js';

it('audits every visible connection field and keeps credentials secret', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://zz.253.com');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('产品概览 > API 接口信息');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['account', 'password']);
  for (const field of manifest.spec.auth.fields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://zz.253.com'); expect(field.description).toContain('blank'); }
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toContain('Leave blank'); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['getBalance']);
  const credentials = { account: new SecretString('N9000123'), password: new SecretString('secret-password') };
  const client = new ChuanglanClient({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain('secret-password');
  expect(() => new ChuanglanClient({ maxResponseBytes: -1 }, { resolve: async () => credentials })).toThrow();
  expect(() => new ChuanglanClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
