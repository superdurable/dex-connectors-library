// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { buildSendRequest, parseSender, twilioTime, TwilioClient, zeroMessage } from '../src/client.js';

const accountSid = 'AC00000000000000000000000000000000';
const messageSid = 'SMfedcba9876543210fedcba9876543210';
const credentialProvider = { resolve: async () => ({ authMethodId: 'auth-token', authToken: new SecretString('auth-token-sentinel-0123456789ab') }) };

function client() { return new TwilioClient({ accountSid, defaultSender: '+14155550100' }, credentialProvider); }

describe('Twilio message validation', () => {
  it('encodes only the Go form fields and preserves media order', () => {
    const request = buildSendRequest({ to: '+14155550123', body: 'Ready now', mediaUrls: ['https://cdn.example/a', 'https://cdn.example/b'] }, parseSender('+14155550100'));
    expect(request.form.toString()).toBe('Body=Ready+now&From=%2B14155550100&MediaUrl=https%3A%2F%2Fcdn.example%2Fa&MediaUrl=https%3A%2F%2Fcdn.example%2Fb&To=%2B14155550123');
  });
  it('refuses a mismatched WhatsApp sender and credential-bearing media URL', () => {
    expect(() => buildSendRequest({ to: 'whatsapp:+14155550123', body: 'hello' }, parseSender('+14155550100'))).toThrow();
    expect(() => buildSendRequest({ to: '+14155550123', mediaUrls: ['https://user:pass@cdn.example/a'] }, parseSender('+14155550100'))).toThrow();
  });
  it('rejects wrong-typed provider fields without copying them to output', () => {
    const body = { sid: messageSid, account_sid: accountSid, to: '+14155550123', status: 'queued', num_segments: { value: 1 } };
    expect(() => client().decodeMessage(Buffer.from(JSON.stringify(body)), { username: accountSid, password: 'secret' })).toThrow('invalid message');
    expect(zeroMessage).toEqual({ accountSid: '', to: '', createdAt: '0001-01-01T00:00:00Z', sentAt: '0001-01-01T00:00:00Z', updatedAt: '0001-01-01T00:00:00Z' });
  });
  it('rejects impossible Twilio dates and preserves a valid offset', () => {
    expect(twilioTime('Mon, 30 Feb 2026 17:04:05 +0000')).toBe('0001-01-01T00:00:00Z');
    expect(twilioTime('Wed, 30 Sep 2026 17:04:05 +0100')).toBe('2026-09-30T16:04:05Z');
  });
});
