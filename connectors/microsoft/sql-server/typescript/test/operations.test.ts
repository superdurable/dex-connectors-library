// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { SQLServerClient, classifyDriverError } from '../src/client.js';

const client = new SQLServerClient({ host: '127.0.0.1', database: 'master', user: 'sa', encrypt: 'disable' }, {
  resolve: async () => ({ password: new SecretString('inert-test-password') }),
});

describe('Go-compatible SQL Server error decisions', () => {
  const cases = [
    [18456, 'rejected', 'AUTHENTICATION'], [18487, 'rejected', 'AUTHENTICATION'], [40615, 'rejected', 'AUTHENTICATION'],
    [4060, 'rejected', 'NOT_FOUND'], [229, 'rejected', 'AUTHORIZATION'], [3906, 'rejected', 'AUTHORIZATION'],
    [208, 'rejected', 'NOT_FOUND'], [2627, 'rejected', 'CONFLICT'], [2601, 'rejected', 'CONFLICT'],
    [547, 'rejected', 'CONFLICT'], [515, 'rejected', 'CONFLICT'], [2628, 'rejected', 'VALIDATION'],
    [245, 'rejected', 'VALIDATION'], [334, 'rejected', 'VALIDATION'], [40544, 'rejected', 'QUOTA_EXHAUSTED'],
    [40549, 'rejected', 'AVAILABILITY'], [1205, 'retry', 'CONFLICT'], [1222, 'retry', 'CONFLICT'],
    [3960, 'retry', 'CONFLICT'], [9002, 'retry', 'AVAILABILITY'], [40501, 'retry', 'AVAILABILITY'],
    [40197, 'retry', 'AVAILABILITY'], [40613, 'retry', 'AVAILABILITY'], [50999, 'retry', 'AVAILABILITY'],
    [50000, 'rejected', 'PROVIDER_REJECTION'],
  ] as const;
  for (const [number, disposition, kind] of cases) {
    it(`classifies server error ${number}`, () => {
      const actual = classifyDriverError({ number, class: number === 50999 ? 21 : 16, state: 3, message: 'secret-card-4242' }, 'statement');
      expect(actual.disposition).toBe(disposition);
      expect(actual.kind).toBe(kind);
      expect(actual.message).not.toContain('secret-card-4242');
    });
  }
  it('treats only an unconfirmed COMMIT as uncertain', () => {
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'statement').disposition).toBe('retry');
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'commit').disposition).toBe('uncertain');
    expect(classifyDriverError({ number: 3999, class: 17 }, 'commit').disposition).toBe('uncertain');
    expect(classifyDriverError({ number: 3902, class: 16 }, 'commit').disposition).toBe('rejected');
    expect(classifyDriverError({ number: 1205, class: 13 }, 'commit').disposition).toBe('retry');
  });
  it('separates credential, DNS, and transient dial failures', () => {
    expect(classifyDriverError({ code: 'ELOGIN' }, 'connect').kind).toBe('AUTHENTICATION');
    expect(classifyDriverError({ code: 'ENOTFOUND' }, 'connect').kind).toBe('NOT_FOUND');
    expect(classifyDriverError({ code: 'ECONNREFUSED' }, 'connect').disposition).toBe('retry');
    expect(classifyDriverError({ code: 'ERR_TLS_CERT_ALTNAME_INVALID' }, 'connect').disposition).toBe('rejected');
  });
});

it('rejects unsafe configuration before opening a socket', () => {
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('loopback');
  expect(client.query().definition().operation.operationId).toBe('query');
  expect(client.execute().definition().operation.operationId).toBe('execute');
});
