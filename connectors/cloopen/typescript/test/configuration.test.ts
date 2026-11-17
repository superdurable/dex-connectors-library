// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import YAML from 'yaml';
import { CloopenClient } from '../src/index.js';

it('audits every visible connection field and keeps both credentials secret', () => {
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; default: unknown; description: string }> }; operations: Array<{ name: string }> } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://console.yuntongxun.com');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('ACCOUNT SID');
  expect(manifest.spec.auth.fields.map(field => field.name)).toEqual(['account_sid', 'auth_token']);
  for (const field of manifest.spec.auth.fields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://console.yuntongxun.com'); expect(field.description).toContain('Blank'); }
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toContain('Leave blank'); }
  expect(manifest.spec.operations.map(operation => operation.name)).toEqual(['getBalance']);
  const credentials = { accountSid: new SecretString('abcdefghijklmnopqrstuvwxyz012345'), authToken: new SecretString('012345abcdefghijklmnopqrstuvwxyz') };
  const client = new CloopenClient({}, { resolve: async () => credentials });
  expect(JSON.stringify(client)).not.toContain(credentials.accountSid.reveal());
  expect(JSON.stringify(client)).not.toContain(credentials.authToken.reveal());
  expect(() => new CloopenClient({ maxResponseBytes: -1 }, { resolve: async () => credentials })).toThrow();
  expect(() => new CloopenClient({ timeoutMs: 0 }, { resolve: async () => credentials })).toThrow();
});
