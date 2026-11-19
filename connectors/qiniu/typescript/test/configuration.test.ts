// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { QiniuClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'qiniu', name: 'bucket-list' };
const credentials = new StaticCredentialProvider([[reference, {
  accessKey: new SecretString('test-access-key'), secretKey: new SecretString('test-secret-key'),
}]]);

describe('Dex Web connection configuration', () => {
  it('documents the UC origin, defaults, and both secret signing keys', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map((field: { name: string }) => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'access_key', 'secret_key']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://portal.qiniu.com/user/key');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://uc.qiniuapi.com', '10s', 262144]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe origins and keeps signing credentials private', () => {
    expect(() => new QiniuClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Qiniu UC URL');
    expect(() => new QiniuClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Qiniu UC URL');
    expect(() => decodeCredentials({ access_key: 3, secret_key: 'test' })).toThrowError(/access_key has an invalid type/);
    expect(String(new SecretString('test-secret-key'))).not.toContain('test-secret-key');
  });
});
