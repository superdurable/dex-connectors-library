// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { HeartbeatContext } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { buildSendRequest, parseSender, twilioFailure, twilioTime, TwilioClient, zeroMessage } from '../src/client.js';

const accountSid = 'AC00000000000000000000000000000000';
const messageSid = 'SMfedcba9876543210fedcba9876543210';
const credentialProvider = { resolve: async () => ({ authMethodId: 'auth-token', authToken: new SecretString('auth-token-sentinel-0123456789ab') }) };

function client() { return new TwilioClient({ accountSid, defaultSender: '+14155550100' }, credentialProvider); }

describe('Twilio message validation', () => {
  it('encodes only the Go form fields and preserves media order', () => {
    const request = buildSendRequest({ to: '+14155550123', body: 'Ready now', mediaUrls: ['https://cdn.example/a', 'https://cdn.example/b'] }, parseSender('+14155550100'));
    expect(request.form.toString()).toBe('Body=Ready+now&From=%2B14155550100&MediaUrl=https%3A%2F%2Fcdn.example%2Fa&MediaUrl=https%3A%2F%2Fcdn.example%2Fb&To=%2B14155550123');
  });
  it('uses Go query escaping on the sent wire body', async () => {
    let body = '';
    const sender = new TwilioClient({ accountSid }, credentialProvider, { send: async request => {
      body = Buffer.from(request.body ?? '').toString('utf8');
      return { status: 201, headers: {}, body: Buffer.alloc(0) };
    } });
    const request = buildSendRequest({ to: '+14155550123', body: '50%* off~', mediaUrls: ['https://cdn.example/a~b'] }, parseSender('+14155550100'));
    await sender.exchange({ context: { cancellationSignal: new AbortController().signal } } as Call,
      { username: accountSid, password: 'secret' }, 'POST', sender.messagesURL, request.form);
    expect(body).toBe('Body=50%25%2A+off~&From=%2B14155550100&MediaUrl=https%3A%2F%2Fcdn.example%2Fa~b&To=%2B14155550123');
  });
  it('refuses a mismatched WhatsApp sender and credential-bearing media URL', () => {
    expect(() => buildSendRequest({ to: 'whatsapp:+14155550123', body: 'hello' }, parseSender('+14155550100'))).toThrow();
    expect(() => buildSendRequest({ to: '+14155550123', mediaUrls: ['https://user:pass@cdn.example/a'] }, parseSender('+14155550100'))).toThrow();
    for (const mediaURL of ['https:///x', 'https:host/x', 'https://@host/x']) {
      expect(() => buildSendRequest({ to: '+14155550123', mediaUrls: [mediaURL] }, parseSender('+14155550100'))).toThrow();
    }
  });
  it('rejects wrong-typed provider fields without copying them to output', () => {
    const body = { sid: messageSid, account_sid: accountSid, to: '+14155550123', status: 'queued', num_segments: { value: 1 } };
    expect(() => client().decodeMessage(Buffer.from(JSON.stringify(body)), { username: accountSid, password: 'secret' })).toThrow('invalid message');
    expect(zeroMessage).toEqual({ accountSid: '', to: '', createdAt: '0001-01-01T00:00:00Z', sentAt: '0001-01-01T00:00:00Z', updatedAt: '0001-01-01T00:00:00Z' });
  });
  it('accepts blank integer strings but rejects nondecimal JSON number tokens', () => {
    const prefix = `{"sid":"${messageSid}","account_sid":"${accountSid}","to":"+14155550123","status":"queued",`;
    const authorization = { username: accountSid, password: 'secret' };
    expect(client().decodeMessage(Buffer.from(`${prefix}"num_segments":""}`), authorization).segmentCount).toBeUndefined();
    expect(client().decodeMessage(Buffer.from(`${prefix}"num_segments":5}`), authorization).segmentCount).toBe(5);
    expect(client().decodeMessage(Buffer.from(`${prefix}"num_segments":"05"}`), authorization).segmentCount).toBe(5);
    for (const token of ['5.0', '1e1', '-1']) {
      expect(() => client().decodeMessage(Buffer.from(`${prefix}"num_segments":${token}}`), authorization)).toThrow('invalid message');
    }
  });
  it('classifies a 401 as authentication even when credential lookup fails', async () => {
    const sender = new TwilioClient({ accountSid, defaultSender: '+14155550100' }, credentialProvider, {
      send: async () => ({ status: 401, headers: {}, body: Buffer.from('{"code":20003}') }),
    });
    vi.spyOn(sender, 'authorizationAfterRejection').mockResolvedValue({
      failure: twilioFailure('sendMessage', 'AVAILABILITY', 'Twilio credential refresh is temporarily unavailable'), isTransient: true,
    });
    const call = { id: 'd1ffeecf-f887-5bbc-8ca8-1325f0d0700d', idempotencyKey: 'd1ffeecf-f887-5bbc-8ca8-1325f0d0700d',
      connection: { provider: 'twilio', name: 'twilio-test' }, operation: { connectorId: 'twilio-messaging', operationId: 'sendMessage' },
      context: new HeartbeatContext(undefined), reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false } as unknown as Call;
    expect(await sender.sendMessage().invoke(call, { to: '+14155550123', body: 'hello' })).toMatchObject({
      branch: 'providerRejected', failure: { kind: 'AUTHENTICATION', message: 'Twilio credential refresh is temporarily unavailable' },
    });
  });
  it('rejects impossible Twilio dates and preserves a valid offset', () => {
    expect(twilioTime('Mon, 30 Feb 2026 17:04:05 +0000')).toBe('0001-01-01T00:00:00Z');
    expect(twilioTime('Wed, 30 Sep 2026 17:04:05 +0100')).toBe('2026-09-30T16:04:05Z');
  });
});
