// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { BaiduMapsClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'baidu-maps', name: 'address-lookup' };
const credentials = new StaticCredentialProvider([[reference, { accessKey: new SecretString('test-baidu-server-ak-123') }]]);

describe('Dex Web connection configuration', () => {
  it('gives each visible field provider-owned guidance and safe defaults', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'access_key']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(100);
      expect(field.description).toMatch(/Blank/);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://lbs.baidu.com/apiconsole/key');
    expect(manifest.spec.auth.fields[0].type).toBe('secretString');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://api.map.baidu.com', '10s', 262144]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects invalid origins and keeps AK out of error messages', () => {
    expect(() => new BaiduMapsClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Baidu Maps base URL is invalid');
    expect(() => new BaiduMapsClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Baidu Maps base URL is invalid');
    expect(() => decodeCredentials({ access_key: 123 })).toThrowError(/access_key has an invalid type/);
    expect(String(new SecretString('test-baidu-server-ak-123'))).not.toContain('test-baidu-server-ak-123');
  });
});
