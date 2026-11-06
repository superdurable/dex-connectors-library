// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { encodeGoQuery, escapeGoPath, ZohoClient } from '../src/client.js';
import { buildFindRecordsQuery, buildListModifiedRecordsQuery } from '../src/coql.js';
import { decodeModuleFields, decodeRecordPage, parseZohoTime, validateWriteFields } from '../src/records.js';
import type { CredentialSource } from '../src/models.js';
import { ZohoHTTPClient } from '../src/http.js';

const bytes = (value: unknown) => Buffer.from(JSON.stringify(value));

describe('Go wire parity at provider boundaries', () => {
  it('uses Go path and query escaping for punctuation and Unicode', () => {
    expect(escapeGoPath("!$&'()*+,;=:@?/é")).toBe('%21$&%27%28%29%2A+%2C%3B=:@%3F%2F%C3%A9');
    expect(encodeGoQuery({ z: ['a b~*!'], a: ['x/y'] })).toBe('a=x%2Fy&z=a+b~%2A%21');
  });

  it('accepts leap days and year zero but rejects impossible COQL dates', () => {
    const input = { module: 'Contacts', fields: ['Email'], conditions: [{ field: 'Created_Time', operator: '=', value: { kind: 'date', text: '0000-02-29' } }] };
    expect(buildFindRecordsQuery(input)).toContain("'0000-02-29'");
    expect(() => buildFindRecordsQuery({ ...input, conditions: [{ ...input.conditions[0]!, value: { kind: 'date', text: '2026-02-29' } }] })).toThrow();
    expect(parseZohoTime('2026-02-30T00:00:00+00:00')).toBeUndefined();
    expect(parseZohoTime('2026-01-01T24:00:00+00:00')).toBeUndefined();
  });

  it('rejects wrong-typed provider field metadata instead of dropping it', () => {
    for (const field of [
      { api_name: 'Email', data_type: 'email', custom_field: 'false' },
      { api_name: 'Email', data_type: 'email', pick_list_values: 'bad' },
      { api_name: 'Email', data_type: 'email', lookup: { module: { api_name: 3 } } },
    ]) expect(() => decodeModuleFields(bytes({ fields: [field] }))).toThrow();
  });

  it('keeps Go null collection semantics for record pages', () => {
    expect(decodeRecordPage('Contacts', bytes({ data: null, info: { more_records: null } }))).toEqual({ records: [], hasMore: false });
  });

  it('routes revoked credentials on both writes to providerRejected before sending', async () => {
    const credentials = { resolve: async () => { throw new ReauthorizationRequiredError(); } } as unknown as CredentialSource;
    const client = new ZohoClient({}, credentials);
    const call = { context: { cancellationSignal: new AbortController().signal } } as unknown as Call;
    const upsert = await client.upsertRecord().invoke(call, { module: 'Contacts', fields: { Email: 'ada@example.test' }, duplicateCheckFields: ['Email'] });
    const update = await client.updateRecord().invoke(call, { module: 'Deals', recordId: '123', fields: { Amount: 1250.5 } });
    expect(upsert).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    expect(update).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
  });

  it('accepts exact fractional JSON values and rejects unsafe integer fields', () => {
    expect(validateWriteFields({ Amount: 1250.5 })).toEqual({ Amount: 1250.5 });
    expect(() => validateWriteFields({ Amount: 9007199254740992 })).toThrow('field Amount exceeds exact numeric precision');
  });

  it('rejects normalized cursor dates and treats Go zero time as absent', () => {
    const input = { module: 'Deals', fields: ['Modified_Time'] };
    for (const at of ['2026-02-30T00:00:00Z', '2026-01-01T24:00:00Z']) {
      expect(() => buildListModifiedRecordsQuery({ ...input, cursor: `${at}/123` })).toThrow('cursor must be a cursor returned by listModifiedRecords');
    }
    expect(() => buildListModifiedRecordsQuery({ ...input, modifiedSince: '0001-01-01T00:00:00Z' })).toThrow('modifiedSince or cursor is required');
  });

  it('uses one operation signal for credential resolution and repeated requests', async () => {
    let credentialSignal: AbortSignal | undefined;
    const requestSignals: (AbortSignal | undefined)[] = [];
    const credentials = {
      authMethodId: 'zoho-us-oauth', accessToken: new SecretString('synthetic-access'),
    };
    const source = { resolveWithRefresh: async (_call: Call, _driver: unknown, signal?: AbortSignal) => {
      credentialSignal = signal; return credentials;
    } } as unknown as CredentialSource;
    const http = new ZohoHTTPClient({}, source, { apiBaseURL: 'http://127.0.0.1:12345/crm/v8', transport: async (request: ProviderRequest) => {
      requestSignals.push(request.signal);
      return { status: 200, headers: {}, body: bytes({ data: [] }) };
    } });
    const call = { id: 'shared-deadline', context: { cancellationSignal: new AbortController().signal } } as unknown as Call;
    const session = await http.session(call, 'findRecords');
    if ('outcome' in session) throw new Error('session did not start');
    await http.exchange(call, 'findRecords', session, 'POST', '/coql');
    await http.exchange(call, 'findRecords', session, 'POST', '/coql');
    expect(requestSignals).toEqual([credentialSignal, credentialSignal]);
  });

  it('retains remaining credits on oversized and credential-reflecting success responses', async () => {
    const credentials = { authMethodId: 'zoho-us-oauth', accessToken: new SecretString('synthetic-access') };
    const source = { resolveWithRefresh: async () => credentials } as unknown as CredentialSource;
    const call = { id: 'credit-test', context: { cancellationSignal: new AbortController().signal } } as unknown as Call;
    for (const transport of [
      async () => { throw new BodyTooLargeError(200, { 'X-API-CREDITS-REMAINING': '42' }); },
      async () => ({ status: 200, headers: { 'X-API-CREDITS-REMAINING': '42' }, body: Buffer.from('synthetic-access') }),
    ]) {
      const http = new ZohoHTTPClient({}, source, { apiBaseURL: 'http://127.0.0.1:12345/crm/v8', transport });
      const session = await http.session(call, 'getRecord');
      if ('outcome' in session) throw new Error('session did not start');
      const result = await http.exchange(call, 'getRecord', session, 'GET', '/Deals/123');
      expect(result.outcome).toBe('invalid');
      expect(http.receipt(call, result).metadata).toEqual({ remainingCredits: '42' });
    }
  });
});
