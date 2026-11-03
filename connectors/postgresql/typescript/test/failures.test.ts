// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import pg from 'pg';
import { classifyFailure } from '../src/failures.js';

function databaseError(code: string): pg.DatabaseError {
  const error = new pg.DatabaseError('secret text', 0, 'error');
  error.code = code;
  return error;
}

it.each([
  ['40001', 'retry', 'CONFLICT'], ['40P01', 'retry', 'CONFLICT'], ['55P03', 'retry', 'CONFLICT'],
  ['57P01', 'retry', 'AVAILABILITY'], ['57P03', 'retry', 'AVAILABILITY'], ['53300', 'retry', 'AVAILABILITY'], ['58030', 'retry', 'AVAILABILITY'],
  ['08006', 'retry', 'TRANSPORT'], ['28P01', 'rejected', 'AUTHENTICATION'], ['42501', 'rejected', 'AUTHORIZATION'],
  ['25006', 'rejected', 'AUTHORIZATION'], ['3D000', 'rejected', 'NOT_FOUND'], ['42P01', 'rejected', 'NOT_FOUND'],
  ['23505', 'rejected', 'CONFLICT'], ['23503', 'rejected', 'CONFLICT'], ['57014', 'rejected', 'AVAILABILITY'],
  ['22P02', 'rejected', 'VALIDATION'], ['42601', 'rejected', 'VALIDATION'], ['0A000', 'rejected', 'VALIDATION'],
  ['XX000', 'rejected', 'PROVIDER_REJECTION'],
])('classifies server SQLSTATE %s without returning provider text', (code, disposition, kind) => {
  const result = classifyFailure(databaseError(code), 'the statement');
  expect(result).toMatchObject({ disposition, kind, sqlState: code });
  expect(result.message).not.toContain('secret text');
});

it('distinguishes conclusive and ambiguous COMMIT errors', () => {
  expect(classifyFailure(new Error('socket closed'), 'COMMIT')).toMatchObject({ disposition: 'uncertain', kind: 'TRANSPORT' });
  expect(classifyFailure(databaseError('23505'), 'COMMIT')).toMatchObject({ disposition: 'rejected', kind: 'CONFLICT' });
  expect(classifyFailure(databaseError('40001'), 'COMMIT')).toMatchObject({ disposition: 'retry', kind: 'CONFLICT' });
  expect(classifyFailure(databaseError('57P01'), 'COMMIT')).toMatchObject({ disposition: 'uncertain', kind: 'AVAILABILITY' });
  expect(classifyFailure({ name: 'CommitNotSentError' }, 'COMMIT')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
});

it('classifies certificate, name-resolution, and refused-dial errors separately', () => {
  expect(classifyFailure({ code: 'CERT_HAS_EXPIRED' }, 'connect')).toMatchObject({ disposition: 'rejected', kind: 'AUTHENTICATION' });
  expect(classifyFailure({ code: 'ENOTFOUND' }, 'connect')).toMatchObject({ disposition: 'rejected', kind: 'NOT_FOUND' });
  expect(classifyFailure({ code: 'ECONNREFUSED' }, 'connect')).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY' });
});

it.each(['EPIPE', 'EBADF', 'EPERM', 'ECONNRESET', 'ETIMEDOUT', 'ECONNREFUSED'])('treats %s as transport failure, never SQLSTATE', code => {
  expect(classifyFailure({ code }, 'the statement')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
  expect(classifyFailure({ code }, 'COMMIT')).toMatchObject({ disposition: 'uncertain', kind: 'TRANSPORT' });
});

it.each(['UNABLE_TO_GET_ISSUER_CERT_LOCALLY', 'UNABLE_TO_GET_ISSUER_CERT', 'CERT_NOT_YET_VALID', 'CERT_UNTRUSTED', 'CERT_REVOKED', 'HOSTNAME_MISMATCH'])('rejects TLS verification code %s', code => {
  expect(classifyFailure({ code }, 'connect')).toMatchObject({ disposition: 'rejected', kind: 'AUTHENTICATION' });
});
