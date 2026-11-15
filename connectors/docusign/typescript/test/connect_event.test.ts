// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ConnectMessageMalformedError, ConnectSignatureInvalidError, decodeConnectMessage, verifyConnectSignature } from '../src/index.js';
import { connectBody, connectSignature, testAccountId, testEnvelopeId } from './trigger_support.js';

const receivedAt = new Date('2026-10-05T08:00:00.500Z');
const decode = (body: string) => decodeConnectMessage(Buffer.from(body), receivedAt);
const minimal = (fields: string) => `{"event":"envelope-completed",${fields}"data":{"accountId":"${testAccountId}","envelopeId":"${testEnvelopeId}"}}`;

describe('decodeConnectMessage', () => {
  it('matches keys case-insensitively as Go encoding/json does', () => {
    const event = decode(`{"Event":"envelope-declined","DATA":{"AccountId":"${testAccountId}","envelopeid":"${testEnvelopeId}","envelopeSummary":{"Status":"Declined"}}}`);
    expect(event?.id).toBe(`${testEnvelopeId}:envelope-declined`);
    expect(event?.payload.status).toBe('declined');
  });

  it('falls back to the receipt time for an unparsable generation time', () => {
    expect(decode(minimal('"generatedDateTime":"yesterday",'))?.payload.generatedAt).toBe('2026-10-05T08:00:00.5Z');
    expect(decode(minimal('"generatedDateTime":"2026-10-04T13:00:00+02:00",'))?.occurredAt).toEqual(new Date('2026-10-04T11:00:00Z'));
  });

  it('refuses malformed messages and acknowledges other events unrecorded', () => {
    for (const body of ['', 'null', '[]', '{}', '{"event":"envelope-completed"}', '{"event":"envelope-completed","data":"x"}', '{"event":7,"data":{}}',
      minimal('').replace(testEnvelopeId, 'not-a-guid'), minimal('').replace(`"${testAccountId}"`, '7'),
      connectBody('envelope-completed', testEnvelopeId, 'opp').replace('"textCustomFields":[', '"textCustomFields":[7,'),
      connectBody('envelope-completed', testEnvelopeId, 'opp').replace('"status":"completed"', '"status":true')]) {
      expect(() => decode(body), body).toThrow(ConnectMessageMalformedError);
    }
    expect(decode(minimal('').replace('envelope-completed', 'envelope-sent'))).toBeUndefined();
    expect(decode('{"event":"recipient-completed","data":{"envelopeId":"not-a-guid"}}'), 'identities are checked only for envelope outcomes').toBeUndefined();
  });

  it('skips unnamed custom fields and keeps DocuSign order', () => {
    const body = connectBody('envelope-completed', testEnvelopeId, 'opp-1').replace('"textCustomFields":[', '"textCustomFields":[null,{"name":"","value":"x"},{"name":"first","value":null},');
    expect(decode(body)?.payload.customFields).toEqual([{ name: 'first', value: '' }, { name: 'dexSigningRequestId', value: 'opp-1' }]);
  });
});

describe('verifyConnectSignature', () => {
  const body = Buffer.from('{"event":"envelope-completed"}');
  const signature = connectSignature('key', body.toString());
  it('accepts a repeated header value and refuses non-canonical base64', () => {
    expect(() => verifyConnectSignature(body, { 'x-docusign-signature-01': `bogus, ${signature}` }, 'key')).not.toThrow();
    expect(() => verifyConnectSignature(body, { 'x-docusign-signature-1': signature.replace(/=+$/, '') }, 'key')).toThrow(ConnectSignatureInvalidError);
    expect(() => verifyConnectSignature(body, { 'x-docusign-signature-+1': signature }, 'key')).toThrow(ConnectSignatureInvalidError);
  });
});
