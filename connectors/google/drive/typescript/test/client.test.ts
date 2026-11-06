// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { DriveClient, convertFile, goPathEscape, goQueryEscape, goRFC3339 } from '../src/client.js';

const reference = { provider: 'google', name: 'drive-files' };
const credentials = new StaticCredentialProvider([[reference, { accessToken: new SecretString('test-token') }]]);

describe('Go-compatible request values', () => {
  it('uses Go PathEscape and QueryEscape for reserved characters', () => {
    expect(goPathEscape("a/b!$&'+=:@,;?~*")).toBe('a%2Fb%21$&%27+=:@%2C%3B%3F~%2A');
    expect(goQueryEscape('a b*~+$')).toBe('a+b%2A~%2B%24');
  });
  it('rejects calendar rollover and accepts Go time offsets and comma fractions', () => {
    expect(() => goRFC3339('2026-02-30T00:00:00Z')).toThrow();
    expect(() => goRFC3339('2025-02-29T00:00:00Z')).toThrow();
    expect(() => goRFC3339('2026-01-01T24:00:00Z')).toThrow();
    expect(goRFC3339('2024-02-29T23:59:59,1200+99:59')).toBe('2024-02-29T23:59:59.12+99:59');
  });
  it('rejects wrong-typed provider resource fields', () => {
    expect(() => convertFile({ id: 'file_1', name: 'Plan', mimeType: 'text/plain', size: 7 })).toThrow();
    expect(() => convertFile({ id: 'file_1', name: 'Plan', mimeType: 'text/plain', modifiedTime: '2026-02-30T00:00:00Z' })).toThrow();
  });
  it('validates every manifest connection limit', () => {
    expect(() => new DriveClient({ maxUploadBytes: 5_242_881 }, credentials)).toThrow();
    expect(() => new DriveClient({ searchPageSize: 101 }, credentials)).toThrow();
    expect(new DriveClient({ searchPageSize: 30 }, credentials).searchPageSize).toBe(30);
  });
});
