// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { GiteeClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'gitee', name: 'repository-lookup' };
const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('test-gitee-token') }]]);

describe('Dex Web connection configuration', () => {
  it('documents the API origin, defaults, and secret token', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map((field: { name: string }) => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'access_token']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://gitee.com/profile/personal_access_tokens');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://gitee.com', '10s', 262144]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe origins and keeps the token private', () => {
    expect(() => new GiteeClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Gitee API URL');
    expect(() => new GiteeClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Gitee API URL');
    expect(() => decodeCredentials({ access_token: 3 })).toThrowError(/access_token has an invalid type/);
    expect(String(new SecretString('test-gitee-token'))).not.toContain('test-gitee-token');
  });
});
