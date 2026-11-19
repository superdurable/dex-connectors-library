// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { YouzanClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'youzan', name: 'shop-basic' };
const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('sample-test-token') }]]);

describe('Dex Web connection configuration', () => {
  it('documents the API origin, defaults, and secret token', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map((field: { name: string }) => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'accessToken']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://diy.youzanyun.com/application/list');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://open.youzanyun.com', '15s', 65536]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe origins and keeps the token private', () => {
    expect(() => new YouzanClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Youzan API URL');
    expect(() => new YouzanClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Youzan API URL');
    expect(() => decodeCredentials({ accessToken: 3 })).toThrowError(/accessToken has an invalid type/);
    expect(String(new SecretString('sample-test-token'))).not.toContain('sample-test-token');
  });
});
