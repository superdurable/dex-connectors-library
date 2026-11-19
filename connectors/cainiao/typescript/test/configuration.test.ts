// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { CainiaoClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'cainiao', name: 'waybill-trace' };
const credentials = new StaticCredentialProvider([[reference, {
  appKey: '123456', appSecret: new SecretString('secretKey'), sessionKey: new SecretString('sessionToken'),
}]]);

describe('Dex Web connection configuration', () => {
  it('describes TOP authorization and every visible field', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'appKey', 'appSecret', 'sessionKey']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://developer.alibaba.com');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://eco.taobao.com/router/rest', '15s', 65536]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe router URLs and preserves secret-safe errors', () => {
    expect(() => new CainiaoClient({ baseUrl: 'https://example.com/other' }, credentials)).toThrow('Cainiao TOP URL');
    expect(() => new CainiaoClient({ baseUrl: 'http://example.com/router/rest' }, credentials)).toThrow('Cainiao TOP URL');
    expect(() => decodeCredentials({ appSecret: 4 })).toThrowError(/appSecret has an invalid type/);
    expect(String(new SecretString('secretKey'))).not.toContain('secretKey');
  });
});
