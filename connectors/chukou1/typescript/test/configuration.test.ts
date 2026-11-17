// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { Chukou1Client } from '../src/index.js';

it('audits every visible connection field and keeps the token secret', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://openapi.chukou1.cn/v1/Help/SelfAuthorization');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('Customer Authorization Management');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['access_token']);
  expect(manifest.spec.auth.fields[0]).toMatchObject({ type: 'secretString', description: expect.stringContaining('https://yewu.chukou1.cn/Client/Index.aspx') });
  expect(manifest.spec.auth.fields[0]!.description).toContain('blank');
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toContain('Leave blank'); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['listWarehouses']);
  const credentials = { accessToken: new SecretString('secretTestToken') };
  const client = new Chukou1Client({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain('secretTestToken');
  expect(() => new Chukou1Client({ maxResponseBytes: -1 }, { resolve: async () => credentials })).toThrow();
  expect(() => new Chukou1Client({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
