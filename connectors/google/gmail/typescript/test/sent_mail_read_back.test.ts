// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import { rfcMessageIDAddress } from '../src/message.js';
import { GmailProvider } from '../src/provider.js';
import type { GmailSender } from '../src/send.js';
import { sentMailCandidateQuery } from '../src/sent_mail_read_back.js';
import { FakeGmailSentMail, rawMIMEHeader, SentMailCallID, sentMailOperationCall, type FakeSentCopy } from './gmail_sent_mail_fake.js';

const contextFields = { runId: 'run-1', flowStartedAt: new Date('2026-01-02T00:00:00Z') };
const messageIdAddress = rfcMessageIDAddress(SentMailCallID, contextFields.runId, contextFields.flowStartedAt);
const dexMessageID = `<${messageIdAddress}>`;
const otherRunMessageID = `<${rfcMessageIDAddress(SentMailCallID, 'run-0', contextFields.flowStartedAt)}>`;
const sendInput = { to: ['ada@example.com'], subject: 'Progress', textBody: 'Update' };
const replyInput = { messageId: 'source-1', textBody: 'Answer' };
const recipientHeader = { name: 'To', value: '"Ada" <ada@example.com>' };

/** This call's Sent copy as Gmail stores it: a Gmail-generated Message-Id beside the preserved headers. */
function ownSentCopy(id: string): FakeSentCopy {
  return { id, threadId: `${id}-thread`, headers: [
    recipientHeader, { name: 'Message-Id', value: `<CA${id}@mail.gmail.com>` },
    { name: 'X-Dex-Message-ID', value: dexMessageID }, { name: 'X-Dex-Call-ID', value: SentMailCallID },
  ] };
}

/** A reused Flow ID's earlier run: the same X-Dex-Call-ID with that run's own X-Dex-Message-ID. */
function otherRunSentCopy(id: string): FakeSentCopy {
  return { id, threadId: `${id}-thread`, headers: [
    recipientHeader, { name: 'Message-Id', value: `<CA${id}@mail.gmail.com>` },
    { name: 'X-Dex-Message-ID', value: otherRunMessageID }, { name: 'X-Dex-Call-ID', value: SentMailCallID },
  ] };
}

/** A copy that matches only by X-Dex-Call-ID, which Gmail search cannot match and read-back never trusts. */
function callIDOnlySentCopy(id: string): FakeSentCopy {
  return { id, threadId: `${id}-thread`, headers: [recipientHeader, { name: 'Message-Id', value: `<CA${id}@mail.gmail.com>` }, { name: 'X-Dex-Call-ID', value: SentMailCallID }] };
}

function claimedCheckpoint(sendCount: number): { value?: unknown } {
  return { value: { callId: SentMailCallID, attempt: sendCount, state: 'claimed', sendCount } };
}

describe('GmailProvider.findSentMessage', () => {
  it('reads candidate headers and narrows the search only when recipients are given', async () => {
    const gmail = new FakeGmailSentMail();
    gmail.addSentCopy(ownSentCopy('own-copy'));
    gmail.addSentCopy(otherRunSentCopy('other-run'));
    const provider = new GmailProvider(gmail.httpClient());
    const call = sentMailOperationCall('sendMessage', contextFields);
    expect(await provider.findSentMessage(call, gmail.credentials(), messageIdAddress)).toEqual({ id: 'own-copy', threadId: 'own-copy-thread' });
    expect(await provider.findSentMessage(call, gmail.credentials(), messageIdAddress, undefined, ['"Ada" <ada@example.com>'])).toEqual({ id: 'own-copy', threadId: 'own-copy-thread' });
    expect(await provider.findSentMessage(call, gmail.credentials(), rfcMessageIDAddress(SentMailCallID, 'run-9', contextFields.flowStartedAt))).toBeUndefined();
    expect(gmail.searchQueries).toEqual(['in:sent newer_than:2d', 'in:sent newer_than:2d to:ada@example.com', 'in:sent newer_than:2d']);
    expect(gmail.headerReads).toEqual(['other-run', 'own-copy', 'other-run', 'own-copy', 'other-run', 'own-copy']);
  });
});

describe('Sent Mail candidate query', () => {
  it('names the first recipient only when Gmail search reads its address literally', () => {
    expect(sentMailCandidateQuery([])).toBe('in:sent newer_than:2d');
    expect(sentMailCandidateQuery(['<ada@example.com>', '<bob@example.com>'])).toBe('in:sent newer_than:2d to:ada@example.com');
    expect(sentMailCandidateQuery(['"Doe, John" <j.doe+news@mail.example.com>'])).toBe('in:sent newer_than:2d to:j.doe+news@mail.example.com');
    expect(sentMailCandidateQuery(['=?utf-8?q?Jos=C3=A9?= <jose@example.com>'])).toBe('in:sent newer_than:2d to:jose@example.com');
    expect(sentMailCandidateQuery(["<o'brien@example.com>"])).toBe('in:sent newer_than:2d');
    expect(sentMailCandidateQuery(['<{ada}@example.com>'])).toBe('in:sent newer_than:2d');
  });
});

for (const operationId of ['sendMessage', 'replyToMessage'] as const) {
  const requestName = operationId === 'sendMessage' ? 'send' : 'reply';
  const invoke = (operation: GmailSender, call: Call) => operationId === 'sendMessage'
    ? operation.sendMessage(call, sendInput, new Date(Date.now() + 30_000))
    : operation.replyToMessage(call, replyInput, new Date(Date.now() + 30_000));

  describe(`${operationId} Sent Mail read-back`, () => {
    it('posts X-Dex-Message-ID equal to Message-ID, which survives on the Sent copy', async () => {
      const gmail = new FakeGmailSentMail();
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields))).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: 'gmail-1' } });
      const raw = gmail.postedMessages[0]!;
      expect(rawMIMEHeader(raw, 'X-Dex-Message-ID')).toBe(dexMessageID);
      expect(raw).toContain(`\r\nMessage-ID: ${dexMessageID}\r\nX-Dex-Message-ID: ${dexMessageID}\r\n`);
      expect(gmail.sentMail[0]!.headers).toContainEqual({ name: 'Message-Id', value: '<CAgmail-1@mail.gmail.com>' });
      expect(gmail.sentMail[0]!.headers).toContainEqual({ name: 'X-Dex-Message-ID', value: dexMessageID });
    });

    it('finds the Sent copy after a worker crash once Gmail accepted, without posting again', async () => {
      const gmail = new FakeGmailSentMail({ sendOutcomes: ['acceptedWorkerCrashed'] });
      const checkpoint: { value?: unknown } = {};
      // The crashed attempt never settles; Dex runs attempt 2 on a new worker with the claimed checkpoint.
      void invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 1, checkpoint));
      await vi.waitFor(() => expect(gmail.posts()).toBe(1));
      expect(checkpoint.value).toMatchObject({ state: 'claimed', attempt: 1, sendCount: 1 });
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 2, checkpoint))).toMatchObject({
        kind: 'branch', branch: 'sent', value: { messageId: 'gmail-1', threadId: operationId === 'sendMessage' ? 'gmail-thread-1' : 'source-thread' },
      });
      expect(gmail.posts()).toBe(1);
      expect(gmail.headerReads).toEqual(['gmail-1']);
      expect(gmail.sourceReads).toBe(operationId === 'sendMessage' ? 0 : 1);
    });

    it('skips decoy candidates and matches its own X-Dex-Message-ID', async () => {
      const gmail = new FakeGmailSentMail();
      gmail.addSentCopy(ownSentCopy('own-copy'));
      gmail.addSentCopy(callIDOnlySentCopy('call-id-only'));
      gmail.addSentCopy(otherRunSentCopy('other-run'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 2, claimedCheckpoint(1)))).toMatchObject({
        kind: 'branch', branch: 'sent', value: { messageId: 'own-copy', threadId: 'own-copy-thread' },
      });
      expect(gmail.headerReads).toEqual(['other-run', 'call-id-only', 'own-copy']);
      expect(gmail.searchQueries).toEqual([operationId === 'sendMessage' ? 'in:sent newer_than:2d to:ada@example.com' : 'in:sent newer_than:2d']);
      expect(gmail.posts()).toBe(0);
    });

    it('treats decoys alone as a miss and resends below the send cap', async () => {
      const gmail = new FakeGmailSentMail();
      gmail.addSentCopy(callIDOnlySentCopy('call-id-only'));
      gmail.addSentCopy(otherRunSentCopy('other-run'));
      const checkpoint = claimedCheckpoint(1);
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 2, checkpoint))).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: 'gmail-1' } });
      expect(gmail.posts()).toBe(1);
      expect(gmail.headerReads).toEqual(['other-run', 'call-id-only']);
      expect(checkpoint.value).toMatchObject({ state: 'claimed', attempt: 2, sendCount: 2, providerId: 'gmail-1' });
    });

    it('treats decoys alone as a miss at the send cap and selects uncertain without posting', async () => {
      const gmail = new FakeGmailSentMail();
      gmail.addSentCopy(otherRunSentCopy('other-run'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 4, claimedCheckpoint(3)))).toMatchObject({
        kind: 'uncertain', failure: { kind: 'AVAILABILITY', message: `Gmail sent-mail search found no message, and no further ${requestName} is permitted` },
      });
      expect(gmail.posts()).toBe(0);
      expect(gmail.headerReads).toEqual(['other-run']);
    });

    it('reads each decoy once across the four lookups of one poll', async () => {
      const gmail = new FakeGmailSentMail({ sendOutcomes: ['lost'] });
      gmail.addSentCopy(otherRunSentCopy('other-run'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields))).toMatchObject({
        kind: 'retry', failure: { kind: 'TRANSPORT', message: `Gmail ${requestName} outcome is unknown; the sent-mail search found no message` },
      });
      expect(gmail.searchQueries).toEqual(Array(4).fill('in:sent newer_than:2d to:ada@example.com'));
      expect(gmail.headerReads).toEqual(['other-run']);
    });

    it('matches a Sent copy that keeps the client Message-ID, as some gateways do', async () => {
      const gmail = new FakeGmailSentMail();
      gmail.addSentCopy({ id: 'gateway-copy', threadId: 'gateway-thread', headers: [recipientHeader, { name: 'message-id', value: ` ${dexMessageID} ` }] });
      gmail.addSentCopy(otherRunSentCopy('other-run'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 2, claimedCheckpoint(1)))).toMatchObject({
        kind: 'branch', branch: 'sent', value: { messageId: 'gateway-copy', threadId: 'gateway-thread' },
      });
      expect(gmail.posts()).toBe(0);
    });

    it('keeps the search inconclusive when a candidate header read fails and no other candidate matches', async () => {
      const gmail = new FakeGmailSentMail({ failingHeaderReads: new Map([['unreadable', 500], ['deleted', 404]]) });
      gmail.addSentCopy(otherRunSentCopy('unreadable'));
      gmail.addSentCopy(otherRunSentCopy('deleted'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 4, claimedCheckpoint(3)))).toMatchObject({
        kind: 'uncertain', failure: { kind: 'AVAILABILITY', message: `Gmail sent-mail search did not answer, and no further ${requestName} is permitted` },
      });
      expect(gmail.headerReads).toEqual(['deleted', 'unreadable']);
      expect(gmail.posts()).toBe(0);
    });

    it('treats a candidate deleted after the search as a clean miss', async () => {
      const gmail = new FakeGmailSentMail({ failingHeaderReads: new Map([['deleted', 404]]) });
      gmail.addSentCopy(otherRunSentCopy('deleted'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 4, claimedCheckpoint(3)))).toMatchObject({
        kind: 'uncertain', failure: { message: `Gmail sent-mail search found no message, and no further ${requestName} is permitted` },
      });
    });

    it('matches its own copy after an earlier candidate header read fails', async () => {
      const gmail = new FakeGmailSentMail({ failingHeaderReads: new Map([['unreadable', 500]]) });
      gmail.addSentCopy(ownSentCopy('own-copy'));
      gmail.addSentCopy(otherRunSentCopy('unreadable'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields, 4, claimedCheckpoint(3)))).toMatchObject({
        kind: 'branch', branch: 'sent', value: { messageId: 'own-copy' },
      });
      expect(gmail.headerReads).toEqual(['unreadable', 'own-copy']);
    });

    it('reports an unknown outcome as unanswered when a poll cannot read a candidate', async () => {
      const gmail = new FakeGmailSentMail({ sendOutcomes: ['lost'], failingHeaderReads: new Map([['unreadable', 503]]) });
      gmail.addSentCopy(otherRunSentCopy('unreadable'));
      expect(await invoke(gmail.sender(), sentMailOperationCall(operationId, contextFields))).toMatchObject({
        kind: 'retry', failure: { kind: 'TRANSPORT', message: `Gmail ${requestName} outcome is unknown; the sent-mail search did not answer` },
      });
      // A failed read is not a mismatch, so every lookup reads the candidate again.
      expect(gmail.headerReads).toEqual(Array(4).fill('unreadable'));
    });
  });
}
