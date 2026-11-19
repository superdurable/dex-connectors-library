// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { BilibiliClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'bilibili', name: 'authorized-user' };
const credentials = new StaticCredentialProvider([[reference, {
  clientID: 'client123', appSecret: new SecretString('secret456'), accessToken: new SecretString('token789'),
}]]);

describe('Dex Web connection configuration', () => {
  it('describes every visible setting and credential', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'clientID', 'appSecret', 'accessToken']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/Blank/);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://open.bilibili.com');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['https://member.bilibili.com', '15s', 32768]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects invalid origins and never reveals secrets in errors', () => {
    expect(() => new BilibiliClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Bilibili API URL');
    expect(() => new BilibiliClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Bilibili API URL');
    expect(() => decodeCredentials({ appSecret: 5 })).toThrowError(/appSecret has an invalid type/);
    expect(String(new SecretString('secret456'))).not.toContain('secret456');
  });
});
