// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { GmailSender } from '../src/send.js';
import { rfcMessageIDAddress } from '../src/message.js';
import { FakeGmailSentMail, rawMIMEHeader, SentMailCallID, sentMailOperationCall } from './gmail_sent_mail_fake.js';

const flowStartedAt = new Date('2026-01-02T00:00:00Z');
const timestampedMessageID = '92c961afe927cf6531fd616445d7a0d0f8023d04f8b05fd96c0a7b8d0b53eef5@dex.superdurable.dev';
const runOnlyMessageID = '563d1dc32f8aa987145c20dbb42ed754ef6f3f3f4686be8ec0a6de11d0c9a11b@dex.superdurable.dev';
const sendInput = { to: ['ada@example.com'], subject: 'Progress', textBody: 'Update' };
const replyInput = { messageId: 'source-1', textBody: 'Answer' };

function postedMessageIDs(gmail: FakeGmailSentMail): string[] {
  return gmail.postedMessages.map(raw => /^<([^>]+)>$/.exec(rawMIMEHeader(raw, 'Message-ID') ?? '')?.[1] ?? '');
}

describe('Gmail Message-ID derivation from the Dex call context', () => {
  it('keeps the Go-compatible derivation when flowStartedAt is valid', () => {
    expect(rfcMessageIDAddress(SentMailCallID, 'run-1', flowStartedAt)).toBe(timestampedMessageID);
  });

  it('derives a run-only identity when flowStartedAt is absent or invalid', () => {
    expect(rfcMessageIDAddress(SentMailCallID, 'run-1')).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(SentMailCallID, 'run-1', null)).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(SentMailCallID, 'run-1', new Date(Number.NaN))).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(SentMailCallID, 'run-2')).not.toBe(runOnlyMessageID);
    expect(runOnlyMessageID).not.toBe(timestampedMessageID);
  });

  it('rejects an empty run ID', () => {
    expect(() => rfcMessageIDAddress(SentMailCallID, '')).toThrow('Dex run ID is required');
  });

  for (const operationId of ['sendMessage', 'replyToMessage'] as const) {
    const invoke = (operation: GmailSender, call: Call) => operationId === 'sendMessage'
      ? operation.sendMessage(call, sendInput, new Date(Date.now() + 30_000))
      : operation.replyToMessage(call, replyInput, new Date(Date.now() + 30_000));

    it(`${operationId} sends a stable Message-ID when the context has no flowStartedAt`, async () => {
      const gmail = new FakeGmailSentMail();
      for (let attempt = 0; attempt < 2; attempt++) {
        expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, { runId: 'run-1' }))).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: `gmail-${attempt + 1}` } });
      }
      expect(postedMessageIDs(gmail)).toEqual([runOnlyMessageID, runOnlyMessageID]);
      // A retry finds the newest Sent copy by its X-Dex-Message-ID, because Gmail replaced its Message-ID.
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, { runId: 'run-1' }, 2))).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: 'gmail-2' } });
      expect(gmail.searchQueries).toEqual([operationId === 'sendMessage' ? 'in:sent newer_than:2d to:ada@example.com' : 'in:sent newer_than:2d']);
      expect(gmail.posts()).toBe(2);
    });

    it(`${operationId} keeps the timestamped Message-ID when the context has flowStartedAt`, async () => {
      const gmail = new FakeGmailSentMail();
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, { runId: 'run-1', flowStartedAt }))).toMatchObject({ kind: 'branch', branch: 'sent' });
      expect(postedMessageIDs(gmail)).toEqual([timestampedMessageID]);
      expect(rawMIMEHeader(gmail.postedMessages[0]!, 'X-Dex-Message-ID')).toBe(`<${timestampedMessageID}>`);
    });

    it(`${operationId} returns a local defect without a provider request when the context has no run ID`, async () => {
      const gmail = new FakeGmailSentMail();
      const result = await invoke(gmail.sender(), sentMailOperationCall(operationId, { flowStartedAt }));
      expect(result).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'LOCAL_DEFECT', message: 'the Dex call context has no run ID, so nothing was sent' } });
      expect(gmail.requests).toBe(0);
    });
  }
});
