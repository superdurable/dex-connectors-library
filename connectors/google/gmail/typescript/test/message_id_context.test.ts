// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailHTTPClient } from '../src/http.js';
import { GmailProvider } from '../src/provider.js';
import { GmailSender } from '../src/send.js';
import { rfcMessageIDAddress } from '../src/message.js';
import type { Credentials } from '../src/models.js';

const connection = { provider: 'google', name: 'gmail-inbox' };
const credentials: Credentials = { accessToken: new SecretString('access-token'), primaryEmail: 'owner@example.com' };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };
const callId = '01234567-89ab-5cde-8f01-23456789abcd';
const flowStartedAt = new Date('2026-01-02T00:00:00Z');
const timestampedMessageID = '92c961afe927cf6531fd616445d7a0d0f8023d04f8b05fd96c0a7b8d0b53eef5@dex.superdurable.dev';
const runOnlyMessageID = '563d1dc32f8aa987145c20dbb42ed754ef6f3f3f4686be8ec0a6de11d0c9a11b@dex.superdurable.dev';
const sendInput = { to: ['ada@example.com'], subject: 'Progress', textBody: 'Update' };
const replyInput = { messageId: 'source-1', textBody: 'Answer' };

interface ContextFields { runId?: string; flowStartedAt?: Date | null }

function operationCall(operationId: string, fields: ContextFields, attempt = 1): Call {
  const checkpoint: { value?: unknown } = {};
  return {
    id: callId, idempotencyKey: callId, connection,
    operation: { connectorId: 'gmail', operationId },
    context: {
      attempt, ...fields, cancellationSignal: new AbortController().signal, flowId: 'flow-1',
      hasLastHeartbeatValue: () => checkpoint.value !== undefined,
      getLastHeartbeatValue: () => checkpoint.value,
      recordHeartbeat: async (value: unknown) => { checkpoint.value = value; },
    },
  } as unknown as Call;
}

function response(status: number, body: unknown): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
}

function sourceMessage(): ProviderResponse {
  return response(200, { id: 'source-1', threadId: 'thread-1', internalDate: '1767312000000', payload: { headers: [
    { name: 'Message-ID', value: '<source@example.com>' }, { name: 'From', value: 'Ada <ada@example.com>' }, { name: 'Subject', value: 'Question' },
  ] } });
}

/** Fake Gmail that records each POSTed Message-ID and each Sent Mail search query. */
class FakeGmail {
  readonly postedMessageIDs: string[] = [];
  readonly searchQueries: string[] = [];
  requests = 0;

  sender(): GmailSender {
    const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, new StaticCredentialProvider([[connection, credentials]]), driver, request => this.#handle(request));
    return new GmailSender(http, new GmailProvider(http));
  }

  async #handle(request: ProviderRequest): Promise<ProviderResponse> {
    this.requests++;
    if (request.method === 'POST') {
      const payload = JSON.parse(Buffer.from(request.body ?? []).toString()) as { raw: string };
      const messageID = /Message-ID: <([^>]+)>\r\n/.exec(Buffer.from(payload.raw, 'base64url').toString())?.[1];
      if (!messageID) throw new Error('POST lacks a Message-ID header');
      this.postedMessageIDs.push(messageID);
      return response(200, { id: 'sent-1', threadId: 'thread-1' });
    }
    const url = new URL(request.url);
    const query = url.searchParams.get('q');
    if (query !== null) {
      this.searchQueries.push(query);
      return response(200, {});
    }
    return sourceMessage();
  }
}

describe('Gmail Message-ID derivation from the Dex call context', () => {
  it('keeps the Go-compatible derivation when flowStartedAt is valid', () => {
    expect(rfcMessageIDAddress(callId, 'run-1', flowStartedAt)).toBe(timestampedMessageID);
  });

  it('derives a run-only identity when flowStartedAt is absent or invalid', () => {
    expect(rfcMessageIDAddress(callId, 'run-1')).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(callId, 'run-1', null)).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(callId, 'run-1', new Date(Number.NaN))).toBe(runOnlyMessageID);
    expect(rfcMessageIDAddress(callId, 'run-2')).not.toBe(runOnlyMessageID);
    expect(runOnlyMessageID).not.toBe(timestampedMessageID);
  });

  it('rejects an empty run ID', () => {
    expect(() => rfcMessageIDAddress(callId, '')).toThrow('Dex run ID is required');
  });

  for (const operationId of ['sendMessage', 'replyToMessage'] as const) {
    const invoke = (operation: GmailSender, call: Call) => operationId === 'sendMessage'
      ? operation.sendMessage(call, sendInput, new Date(Date.now() + 30_000))
      : operation.replyToMessage(call, replyInput, new Date(Date.now() + 30_000));

    it(`${operationId} sends a stable Message-ID when the context has no flowStartedAt`, async () => {
      const gmail = new FakeGmail();
      for (let attempt = 0; attempt < 2; attempt++) {
        expect(await invoke(gmail.sender(), operationCall(operationId, { runId: 'run-1' }))).toMatchObject({ kind: 'branch', branch: 'sent', value: { messageId: 'sent-1' } });
      }
      expect(gmail.postedMessageIDs).toEqual([runOnlyMessageID, runOnlyMessageID]);
      await invoke(gmail.sender(), operationCall(operationId, { runId: 'run-1' }, 2));
      expect(gmail.searchQueries[0]).toBe(`in:sent rfc822msgid:${runOnlyMessageID}`);
    });

    it(`${operationId} keeps the timestamped Message-ID when the context has flowStartedAt`, async () => {
      const gmail = new FakeGmail();
      expect(await invoke(gmail.sender(), operationCall(operationId, { runId: 'run-1', flowStartedAt }))).toMatchObject({ kind: 'branch', branch: 'sent' });
      expect(gmail.postedMessageIDs).toEqual([timestampedMessageID]);
    });

    it(`${operationId} returns a local defect without a provider request when the context has no run ID`, async () => {
      const gmail = new FakeGmail();
      const result = await invoke(gmail.sender(), operationCall(operationId, { flowStartedAt }));
      expect(result).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'LOCAL_DEFECT', message: 'the Dex call context has no run ID, so nothing was sent' } });
      expect(gmail.requests).toBe(0);
    });
  }
});
