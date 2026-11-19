// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { BmobClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'bmob', name: 'class-count' };
const credentials = new StaticCredentialProvider([[reference, {
  applicationId: new SecretString('test-bmob-app'), restApiKey: new SecretString('test-bmob-rest-key'),
}]]);

describe('Dex Web connection configuration', () => {
  it('describes the app-specific API origin, defaults, and secrets', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map(field => field.name)).toEqual(['baseUrl', 'timeout', 'maxResponseBytes', 'application_id', 'rest_api_key']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toBe('https://www.bmobapp.com/');
    expect(manifest.spec.configuration.fields[0].required).toBe(true);
    expect(manifest.spec.configuration.fields.slice(1).map((field: { default: unknown }) => field.default)).toEqual(['10s', 65536]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects unsafe origins and keeps credentials secret', () => {
    expect(() => new BmobClient({ baseUrl: 'https://example.com/path' }, credentials)).toThrow('Bmob API URL');
    expect(() => new BmobClient({ baseUrl: 'http://example.com' }, credentials)).toThrow('Bmob API URL');
    expect(() => decodeCredentials({ application_id: 3 })).toThrowError(/application_id has an invalid type/);
    expect(String(new SecretString('test-bmob-rest-key'))).not.toContain('test-bmob-rest-key');
  });
});
