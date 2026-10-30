// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { buildMIMEMessage, buildReplyMIMEMessage, decodeGmailMessage, GmailMessageResourceSchema, rfcMessageIDAddress, validateSendInput } from '../src/index.js';

describe('Gmail message conversion', () => {
  it('decodes nested Gmail text and HTML bodies with threading metadata', () => {
    const resource = GmailMessageResourceSchema.parse({
      id: 'message-1', threadId: 'thread-1', internalDate: '1772615700000', labelIds: ['INBOX'], snippet: 'Hello',
      payload: {
        mimeType: 'multipart/alternative', headers: [
          { name: 'Message-ID', value: '<question@mail.example.com>' }, { name: 'From', value: 'Ada <ada@example.com>' },
          { name: 'Reply-To', value: 'ada.support@example.com' }, { name: 'Subject', value: 'Re: Order' },
        ],
        parts: [
          { mimeType: 'text/plain', body: { data: Buffer.from('Hello').toString('base64url') } },
          { mimeType: 'text/html', body: { data: Buffer.from('<p>Hello</p>').toString('base64url') } },
        ],
      },
    });
    const message = decodeGmailMessage(resource);
    expect(message).toMatchObject({ messageId: 'message-1', threadId: 'thread-1', textBody: 'Hello', htmlBody: '<p>Hello</p>', rfcMessageId: '<question@mail.example.com>', replyTo: 'ada.support@example.com' });
    expect(message.receivedAt).toMatch(/Z$/);
  });

  it('rejects malformed base64url and missing provider identity', () => {
    const base = { id: 'one', threadId: 'thread', internalDate: '1772615700000', payload: { mimeType: 'text/plain', body: { data: '*' } } };
    expect(() => decodeGmailMessage(GmailMessageResourceSchema.parse(base))).toThrow();
    expect(() => decodeGmailMessage(GmailMessageResourceSchema.parse({ ...base, payload: { mimeType: 'text/plain', body: { data: 'QQ==' } } }))).toThrow();
    expect(() => decodeGmailMessage(GmailMessageResourceSchema.parse({ ...base, id: '' }))).toThrow();
  });

  it('validates sender and recipients before creating a raw message', () => {
    const recipients = ['Ada <ada@example.com>'];
    const validated = validateSendInput({ to: recipients, subject: 'Hello', textBody: 'Body' }, 'owner@example.com');
    expect(validated).toEqual(recipients);
    expect(validated).not.toBe(recipients);
    expect(() => validateSendInput({ to: ['a@example.com', 'A@example.com'], subject: 'Hello', textBody: 'Body' }, 'owner@example.com')).toThrow('duplicated');
    expect(() => validateSendInput({ to: ['a@example.com'], subject: 'Hi\nInjected', textBody: 'Body' }, 'owner@example.com')).toThrow();
  });

  it('builds a run-specific Message-ID and a reply in its original thread', () => {
    const messageID = rfcMessageIDAddress('idempotency-key', 'run-1', new Date('2026-01-02T00:00:00Z'));
    expect(messageID).toMatch(/^[0-9a-f]{64}@dex\.superdurable\.dev$/);
    expect(messageID).not.toBe(rfcMessageIDAddress('idempotency-key', 'run-2', new Date('2026-01-02T00:00:00Z')));
    const raw = buildMIMEMessage('call-1', 'idempotency-key', messageID, 'owner@example.com', ['ada@example.com'], 'Hello', 'Line 1\nLine 2');
    expect(Buffer.from(raw).toString()).toContain('Message-ID: <' + messageID + '>\r\n');
    expect(Buffer.from(raw).toString()).toContain('Line 1\r\nLine 2');
    const source = { messageId: 'one', threadId: 'thread', rfcMessageId: '<source@example.com>', from: 'ada@example.com', replyTo: 'reply@example.com', references: '<prior@example.com>', subject: 'Question', receivedAt: '2026-01-02T00:00:00Z' };
    const reply = buildReplyMIMEMessage('call-1', 'idempotency-key', messageID, 'owner@example.com', source, { messageId: 'one', textBody: 'Answer' });
    expect(Buffer.from(reply).toString()).toContain('To: reply@example.com\r\n');
    expect(Buffer.from(reply).toString()).toContain('Subject: Re: Question\r\n');
    expect(Buffer.from(reply).toString()).toContain('References: <prior@example.com> <source@example.com>\r\n');
  });
});
