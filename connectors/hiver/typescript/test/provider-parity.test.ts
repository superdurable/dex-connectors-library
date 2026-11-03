// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { HiverClient } from '../src/client.js';
import { decodeInbox, decodeListPage, decodeNote, pathEscape } from '../src/decode.js';

function noteBody(createdAt: string): Uint8Array {
  return Buffer.from(JSON.stringify({ data: { id: 35042650, created_at: createdAt } }));
}

const call = { id: '00000000-0000-5000-8000-000000000001', context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Hiver provider parity', () => {
  it('keeps Go path escaping for reserved punctuation', () => {
    expect(pathEscape("a!$&'()*+,;=:@?/z~")).toBe('a%21$&%27%28%29%2A+%2C%3B=:@%3F%2Fz~');
  });

  it('retains numeric Hiver IDs beyond JavaScript safe integer range', () => {
    const page = decodeListPage(Buffer.from('{"data":{"results":[{"id":9007199254740993,"display_name":"Support","email":"support@example.com","is_authorised":true}]}}'));
    expect(decodeInbox(page.results[0]).id).toBe('9007199254740993');
  });

  it('validates calendar dates and comma fractional seconds', () => {
    expect(decodeNote(noteBody('2024-02-29T14:24:22,123Z')).createdAt).toBe('2024-02-29T14:24:22.123Z');
    expect(() => decodeNote(noteBody('2025-02-29T14:24:22Z'))).toThrow();
    expect(() => decodeNote(noteBody('2026-02-30T14:24:22Z'))).toThrow();
    expect(() => decodeNote(noteBody('2026-07-21T24:00:00Z'))).toThrow();
  });

  it('accepts Unicode and domain-literal bare emails while rejecting a display name', async () => {
    const requests: string[] = [];
    const client = new HiverClient({ requestIntervalMilliseconds: 1 }, { resolve: async () => ({ apiKey: new SecretString('hiverTestKey0123456789') }) }, {
      send: async request => { requests.push(request.url); return { status: 401, headers: {}, body: Buffer.alloc(0) }; },
    });
    for (const email of ['müller@bücher.example', 'alice@[127.0.0.1]', '"a b"@example.com']) {
      const result = await client.updateConversation().invoke(call, { inboxId: '105902', conversationId: '573741352', assigneeEmail: email });
      expect(result.kind).toBe('branch');
      if (result.kind === 'branch') expect(result.branch).toBe('providerRejected');
    }
    expect(requests).toHaveLength(3);
    const invalid = await client.updateConversation().invoke(call, { inboxId: '105902', conversationId: '573741352', assigneeEmail: 'Alice <alice@example.com>' });
    expect(invalid.kind).toBe('branch');
    if (invalid.kind === 'branch') expect(invalid.branch).toBe('defect');
    expect(requests).toHaveLength(3);
  });
});
