// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { CTYunClient } from '../src/index.js';

it('audits all visible connection fields and keeps AK, SK, and tenant ID private', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string; configurationUI?: unknown }> } };
  expect(manifest.spec.auth.guide.startURL).toContain('ctyun.cn/document/');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('tenantId');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['access_key', 'security_key', 'tenant_id']);
  for (const field of manifest.spec.auth.fields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://www.ctyun.cn/'); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toMatch(/blank/i); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['listKnowledgeBases']);
  expect(manifest.spec.operations[0]?.configurationUI).toBeUndefined();
  const credentials = { accessKey: new SecretString('secret-access'), securityKey: new SecretString('secret-security'), tenantId: new SecretString('secret-tenant') };
  const client = new CTYunClient({}, { resolve: async () => credentials });
  for (const value of Object.values(credentials)) expect(JSON.stringify(client)).not.toContain(value.reveal());
  expect(() => new CTYunClient({ baseUrl: 'https://other.example' }, { resolve: async () => credentials })).toThrow();
  expect(() => new CTYunClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
