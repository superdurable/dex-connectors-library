// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { AsyncContext } from '@superdurable/dex';
import { stepRef } from '@superdurable/connectors-sdk';
import { newGetMessageStep, newReplyToMessageStep, newSendMessageStep } from '../src/index.js';
import { GmailMock } from '../src/mock/index.js';

const ConnectionName = 'gmail-inbox';
const annotations = { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Call the selected Gmail operation.' };
const context = {
  flowId: 'gmail-example-1', runId: 'run-1', flowStartedAt: new Date('2026-01-02T00:00:00Z'),
  stepExecutionId: 'gmail:1', attempt: 1, deadline: new Date(Date.now() + 30_000),
  cancellationSignal: new AbortController().signal,
} as AsyncContext;

describe('Gmail operation-specific Dex Step factories', () => {
  it('routes each happy-path mock through its typed Step with manifest Execute defaults', async () => {
    const mock = new GmailMock(ConnectionName);
    const connection = mock.connection();
    const read = newGetMessageStep({ stepType: 'ReadGmailMessage', annotations, connectionName: ConnectionName, connection,
      mapToOperationInput: (input: { messageId: string }) => input, read: stepRef('ReadDone') });
    const send = newSendMessageStep({ stepType: 'SendGmailMessage', annotations, connectionName: ConnectionName, connection,
      mapToOperationInput: (input: { to: string[]; subject: string; textBody: string }) => input, sent: stepRef('SendDone') });
    const reply = newReplyToMessageStep({ stepType: 'ReplyToGmailMessage', annotations, connectionName: ConnectionName, connection,
      mapToOperationInput: (input: { messageId: string; textBody: string }) => input, sent: stepRef('ReplyDone') });
    expect(read.getStepOptions().executeMethodTimeoutMs).toBe(30_000);
    expect(send.getStepOptions().executeRetry?.maximumAttempts).toBe(5);
    expect(reply.getStepOptions().executeRetry?.totalDurationMs).toBe(120_000);
    for (const [decision, branch] of [
      [await read.execute(context, { messageId: 'one' }), 'read'],
      [await send.execute(context, { to: ['ada@example.com'], subject: 'Hello', textBody: 'Update' }), 'sent'],
      [await reply.execute(context, { messageId: 'one', textBody: 'Answer' }), 'sent'],
    ] as const) {
      expect(decision.kind).toBe('next');
      if (decision.kind === 'next') expect(decision.movements[0]?.input).toMatchObject({ branch });
    }
    mock.getMessage().assertNoFailures();
    mock.sendMessage().assertNoFailures();
    mock.replyToMessage().assertNoFailures();
  });

  it('rejects a connection name different from the application constant', () => {
    const connection = new GmailMock(ConnectionName).connection();
    expect(() => newGetMessageStep({ stepType: 'ReadGmailMessage', annotations, connectionName: 'other', connection,
      mapToOperationInput: (input: { messageId: string }) => input, read: stepRef('Done') })).toThrow('connection name');
  });
});
