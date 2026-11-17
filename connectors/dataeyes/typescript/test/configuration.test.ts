// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { DataEyesClient } from '../src/index.js';

it('audits every visible connection field and keeps the reading Key private', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string; configurationUI?: unknown }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://doc.shuyanai.com/doc-8441886');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('接入与密钥');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['apiKey']);
  const keyField = manifest.spec.auth.fields[0]!;
  expect(keyField.type).toBe('secretString');
  expect(keyField.description).toContain('https://doc.shuyanai.com/doc-8441886');
  expect(keyField.description).toMatch(/(B|b)lank/);
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toMatch(/(B|b)lank/); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['readWebPage']);
  expect(manifest.spec.operations[0]?.configurationUI).toBeUndefined();
  const credentials = { apiKey: new SecretString('private-reading-key') };
  const client = new DataEyesClient({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain(credentials.apiKey.reveal());
  expect(() => new DataEyesClient({ maxResponseBytes: 16 * 1024 * 1024 + 1 }, { resolve: async () => credentials })).toThrow();
  expect(() => new DataEyesClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
