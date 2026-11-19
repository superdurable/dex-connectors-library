// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import YAML from 'yaml';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { HuaweiCloudClient } from '../src/client.js';
import { decodeCredentials } from '../src/generated/models.js';

const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8'));
const reference = { provider: 'huaweicloud', name: 'project-list' };
const credentials = new StaticCredentialProvider([[reference, {
  accountName: new SecretString('example-account'), iamUsername: new SecretString('example-user'), iamPassword: new SecretString('synthetic-test-password'),
}]]);

describe('Dex Web connection configuration', () => {
  it('documents request limits and all IAM user secrets', () => {
    const fields = [...manifest.spec.configuration.fields, ...manifest.spec.auth.fields];
    expect(fields.map((field: { name: string }) => field.name)).toEqual(['timeout', 'maxResponseBytes', 'account_name', 'iam_username', 'iam_password']);
    for (const field of fields) {
      expect(field.description.length).toBeGreaterThan(80);
      expect(field.description).toMatch(/blank/i);
    }
    expect(manifest.spec.auth.guide.startURL).toContain('support.huaweicloud.com');
    expect(manifest.spec.configuration.fields.map((field: { default: unknown }) => field.default)).toEqual(['10s', 1048576]);
    expect(manifest.spec.operations[0].configurationUI ?? []).toEqual([]);
  });
  it('rejects invalid limits and keeps the IAM password private', () => {
    expect(() => new HuaweiCloudClient({ timeoutMs: 0 }, credentials)).toThrow('Huawei Cloud request limits');
    expect(() => decodeCredentials({ account_name: 3 })).toThrowError(/account_name has an invalid type/);
    expect(String(new SecretString('synthetic-test-password'))).not.toContain('synthetic-test-password');
  });
});
