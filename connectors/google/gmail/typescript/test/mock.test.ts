// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import * as gmailmock from '../src/mock/index.js';

describe('manifest Gmail mock cases', () => {
  it('installs the three happy-path defaults', () => {
    expect(gmailmock.getMessageDefault()).toMatchObject({ kind: 'branch', branch: 'read', value: { messageId: '18c0a1b2c3d4e5f6' } });
    expect(gmailmock.sendMessageDefault()).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: '18c0a1b2c3d4e600' } });
    expect(gmailmock.replyToMessageDefault()).toMatchObject({ kind: 'branch', branch: 'sent', value: { threadId: '18c0a1b2c3d4e5f0' } });
  });

  it('matches every declared exceptional branch and retry', () => {
    expect(gmailmock.getMessageDeletedMessage()).toMatchObject({ branch: 'notFound', failure: { kind: 'NOT_FOUND' } });
    expect(gmailmock.getMessageRevokedGrant()).toMatchObject({ branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
    expect(gmailmock.getMessageMalformedPayload()).toMatchObject({ branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    expect(gmailmock.getMessageEmptyMessageID()).toMatchObject({ branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(gmailmock.getMessageRateLimited()).toMatchObject({ kind: 'retry', retryAfterMs: 1000 });
    expect(gmailmock.sendMessageInvalidRecipient()).toMatchObject({ branch: 'providerRejected' });
    expect(gmailmock.sendMessageNoRecipients()).toMatchObject({ branch: 'defect' });
    expect(gmailmock.sendMessageConnectionLost()).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    expect(gmailmock.sendMessageUnavailable()).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(gmailmock.replyToMessageSourceMissing()).toMatchObject({ branch: 'providerRejected', failure: { kind: 'NOT_FOUND' } });
    expect(gmailmock.replyToMessageMalformedSource()).toMatchObject({ branch: 'invalidResponse' });
    expect(gmailmock.replyToMessageEmptyBody()).toMatchObject({ branch: 'defect' });
    expect(gmailmock.replyToMessageConnectionLost()).toMatchObject({ kind: 'retry' });
  });

  it('scripts a typed named connection with retry then terminal idempotency replay', () => {
    const mock = new gmailmock.GmailMock('gmail-inbox');
    expect(mock.connection().reference).toEqual({ provider: 'google', name: 'gmail-inbox' });
    const call = {
      id: '01234567-89ab-5cde-8f01-23456789abcd', idempotencyKey: 'stable-key', connection: mock.connection().reference,
      operation: { connectorId: 'gmail', operationId: 'sendMessage' }, context: { flowId: 'flow-1' },
    } as Call;
    mock.sendMessage().respond(gmailmock.sendMessageConnectionLost(), gmailmock.sendMessageSentToCustomer());
    const input = { to: ['ada@example.com'], subject: 'Hello', textBody: 'Body' };
    expect(mock.sendMessage().invoke(call, input)).toMatchObject({ kind: 'retry' });
    expect(mock.sendMessage().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'sent' });
    expect(mock.sendMessage().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'sent' });
    expect(mock.sendMessage().calls()).toHaveLength(3);
    expect(mock.sendMessage().calls()[2]).toMatchObject({ replay: true });
  });
});
