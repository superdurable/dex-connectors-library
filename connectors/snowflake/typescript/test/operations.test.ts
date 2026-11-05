// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync, createPublicKey, verify } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SnowflakeClient } from '../src/client.js';

const handle = '00000000-0000-4000-8000-000000000003';
const call = { id: '00000000-0000-4000-8000-000000000004', idempotencyKey: '00000000-0000-4000-8000-000000000004', context: { cancellationSignal: new AbortController().signal }, operation: { connectorId: 'snowflake', operationId: 'submitStatement' }, connection: { provider: 'snowflake', name: 'warehouse' } } as Call;

describe('Snowflake SQL API adapter', () => {
  it('signs a fresh key-pair JWT and binds values outside SQL text', async () => {
    const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
    const requests: ProviderRequest[] = [];
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics', warehouse: 'REPORTING_WH' }, {
      resolve: async () => ({ authMethodId: 'key-pair', user: 'dex_service', privateKey: new SecretString(privateKey.export({ type: 'pkcs8', format: 'pem' }).toString()) }),
    }, { localProviderUrl: 'http://127.0.0.1', now: () => new Date('2026-09-30T12:00:00Z'), send: async request => {
      requests.push(request);
      return { status: 202, headers: {}, body: Buffer.from(JSON.stringify({ statementHandle: handle })) };
    } });
    const result = await client.submitStatement().invoke(call, { statement: "SELECT ?, '?' -- ?", parameters: ['account-secret'] });
    expect(result.kind).toBe('branch');
    const request = requests[0]!;
    expect(new URL(request.url).searchParams.get('requestId')).toBe(call.id);
    expect(JSON.parse(Buffer.from(request.body!).toString())).toMatchObject({ statement: "SELECT ?, '?' -- ?", bindings: { '1': { type: 'TEXT', value: 'account-secret' } }, warehouse: 'REPORTING_WH', timeout: 3600 });
    const token = request.headers!.Authorization!.slice('Bearer '.length);
    const [header, claims, signature] = token.split('.');
    expect(JSON.parse(Buffer.from(claims!, 'base64url').toString())).toMatchObject({ sub: 'MYORG-ANALYTICS.DEX_SERVICE', iat: 1790769600 });
    expect(verify('RSA-SHA256', Buffer.from(`${header}.${claims}`), createPublicKey(privateKey), Buffer.from(signature!, 'base64url'))).toBe(true);
  });

  it('rejects malformed bindings before contacting the provider', async () => {
    let requests = 0;
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics' }, { resolve: async () => ({ authMethodId: 'programmatic-access-token', programmaticAccessToken: new SecretString('token') }) }, {
      localProviderUrl: 'http://127.0.0.1', send: async () => { requests++; throw new Error('unexpected request'); },
    });
    const result = await client.submitStatement().invoke(call, { statement: "SELECT '?' /* ? */", parameters: ['extra'] });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('defect');
    expect(requests).toBe(0);
  });

  it('keeps a wrong-typed provider row on the invalidResponse branch', async () => {
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics' }, { resolve: async () => ({ authMethodId: 'programmatic-access-token', programmaticAccessToken: new SecretString('token') }) }, {
      localProviderUrl: 'http://127.0.0.1', send: async () => ({ status: 200, headers: {}, body: Buffer.from(JSON.stringify({ resultSetMetaData: { format: 'jsonv2', rowType: [{ name: 'VALUE', type: 'fixed', precision: 18 }] }, data: [[12]] })) }),
    });
    const result = await client.getStatementResult().invoke(call, { statementHandle: handle });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
  });

  it('rejects a credential without an authentication method before sending', async () => {
    let requests = 0;
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics' }, {
      resolve: async () => ({ user: 'dex_service', privateKey: new SecretString('unused') }),
    }, { send: async () => { requests++; throw new Error('unexpected request'); } });
    const result = await client.getStatementResult().invoke(call, { statementHandle: handle });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('defect');
      expect(result.failure?.message).toBe('the Snowflake connection selects no supported authentication method');
    }
    expect(requests).toBe(0);
  });

  it('explains how to export an encrypted private key', async () => {
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics' }, {
      resolve: async () => ({ authMethodId: 'key-pair', user: 'dex_service', privateKey: new SecretString('-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIB\n-----END ENCRYPTED PRIVATE KEY-----') }),
    }, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.getStatementResult().invoke(call, { statementHandle: handle });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.failure?.message).toBe('the Snowflake private key is encrypted; export an unencrypted PKCS #8 key with openssl pkcs8 -topk8 -nocrypt');
  });

  it('preserves Go JSON digits in semi-structured columns and reports the UTC zone', async () => {
    const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics' }, {
      resolve: async () => ({ authMethodId: 'programmatic-access-token', programmaticAccessToken: new SecretString('token') }),
    }, { send: async () => ({ status: 200, headers: {}, body: Buffer.from(JSON.stringify({
      resultSetMetaData: { format: 'jsonv2', rowType: [
        { name: 'VARIANT', type: 'variant' }, { name: 'OBJECT', type: 'object' },
        { name: 'ARRAY', type: 'array' }, { name: 'MAP', type: 'map' }, { name: 'SCALAR', type: 'variant' },
        { name: 'EXPONENT', type: 'variant' }, { name: 'TZ', type: 'timestamp_tz' },
      ] }, data: [['{"large":12345678901234567890,"decimal":1.50,"negativeZero":-0}', '{"value":1.50}', '[12345678901234567890]', '{"value":-0}', '1.50', '1e400', '1700000000.123456789 1440']],
    })) }) });
    const result = await client.getStatementResult().invoke(call, { statementHandle: handle });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('completed');
      expect(JSON.stringify(result.value.rows?.[0])).toBe('{"VARIANT":{"large":12345678901234567890,"decimal":1.50,"negativeZero":-0},"OBJECT":{"value":1.50},"ARRAY":[12345678901234567890],"MAP":{"value":-0},"SCALAR":1.50,"EXPONENT":1e400,"TZ":"2023-11-14T22:13:20.123456789Z"}');
    }
  });
});
