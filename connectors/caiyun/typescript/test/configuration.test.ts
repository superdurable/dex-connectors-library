// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { CaiyunClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'caiyun', name: 'current-weather' };
const credentials = new StaticCredentialProvider([[reference, {
  appKey: new SecretString('test-app-key'), appSecret: new SecretString('test-app-secret'),
}]]);

describe('Dex Web connection configuration', () => {
  it('describes Caiyun credentials, defaults, and every visible field', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'app_key', 'app_secret']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://platform.caiyunapp.com');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://api.caiyunapp.com', '10s', 262144]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe origins and preserves secret-safe errors', () => {
    expect(() => new CaiyunClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Caiyun API URL');
    expect(() => new CaiyunClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Caiyun API URL');
    expect(() => decodeCredentials({ app_key: 5 })).toThrowError(/app_key has an invalid type/);
    expect(String(new SecretString('test-app-secret'))).not.toContain('test-app-secret');
  });
});
