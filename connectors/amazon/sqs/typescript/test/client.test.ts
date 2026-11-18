// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { SQSClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';

const queueUrl = 'https://sqs.eu-west-1.amazonaws.com/123456789012/orders.fifo';
const call: Call = {
  id: '00000000-0000-5000-8000-000000000001', idempotencyKey: '00000000-0000-5000-8000-000000000001',
  connection: { provider: 'amazon-sqs', name: 'orders' }, operation: { connectorId: 'amazon-sqs', operationId: 'deleteMessage' },
  context: { flowId: 'sqs-client-test', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

function credentials(secret: string) { return { accessKeyId: new SecretString('AKIAEXAMPLE'), secretAccessKey: new SecretString(secret) }; }

describe('SQS client', () => {
  it('rejects unsafe endpoint and queue configuration before provider access', () => {
    const source = { resolve: async () => credentials('example-secret') };
    expect(() => new SQSClient({ region: 'EU-WEST-1' }, source)).toThrow();
    expect(() => new SQSClient({ region: 'eu-west-1', endpoint: 'http://example.com' }, source)).toThrow();
    expect(() => new SQSClient({ region: 'eu-west-1', defaultQueueUrl: 'https://sqs.us-east-1.amazonaws.com/123456789012/orders' }, source)).toThrow();
    expect(() => new SQSClient({ region: 'eu-west-1', maxMessagesPerReceive: 11 }, source)).toThrow();
    expect(() => new SQSClient({ region: 'eu-west-1', receiveWaitSeconds: 21 }, source)).toThrow();
    expect(() => new SQSClient({ region: 'eu-west-1', maxResponseBytes: -1 }, source)).toThrow();
  });

  it.each([
    ['reauthorization required', new ReauthorizationRequiredError(), true],
    ['connection object not found', new ObjectNotFoundError(), true],
    ['connection not selected', new ConnectionNotSelectedError(), true],
    ['credential broker rejected', new CredentialBrokerRejectedError(), true],
    ['malformed credential material', new CredentialMaterialError('credential access_key_id is required'), true],
    ['SDK provider decoding failure', new TypeError('project credential decoding failed'), true],
    ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
    ['static connection not configured', new Error('connection is not configured'), false],
    ['unlisted error', new TypeError('unlisted'), false],
  ])('classifies a %s credential failure without sending', async (_name, credentialFailure, isTerminal) => {
    let requests = 0;
    const client = new SQSClient({ region: 'eu-west-1', endpoint: 'http://127.0.0.1:21096', defaultQueueUrl: queueUrl }, { resolve: async () => { throw credentialFailure; } }, {
      send: async () => { requests++; throw new Error('unexpected request'); },
    });
    const expected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
    expect(await client.deleteMessage().invoke(call, { receiptHandle: 'receipt-1' })).toMatchObject(expected);
    expect(requests).toBe(0);
  });

  it('rereads credentials, signs the selected queue, and keeps provider text out of failures', async () => {
    let resolves = 0;
    const signatures: string[] = [];
    const client = new SQSClient({ region: 'eu-west-1', endpoint: 'http://127.0.0.1:21096', defaultQueueUrl: queueUrl }, {
      resolve: async () => credentials(++resolves === 1 ? 'first-secret' : 'second-secret'),
    }, { now: () => new Date('2026-10-30T00:00:00Z'), send: async request => {
      signatures.push(request.headers?.Authorization ?? '');
      expect(request.headers?.['X-Amz-Target']).toBe('AmazonSQS.DeleteMessage');
      expect(JSON.parse(Buffer.from(request.body ?? []).toString())).toEqual({ QueueUrl: queueUrl, ReceiptHandle: 'receipt-1' });
      return { status: 403, headers: {}, body: Buffer.from('{"__type":"AccessDenied","message":"second-secret SENTINEL"}') };
    } });
    for (let attempt = 0; attempt < 2; attempt++) {
      const result = await client.deleteMessage().invoke(call, { receiptHandle: 'receipt-1' });
      expect(result.kind).toBe('branch');
      if (result.kind === 'branch') {
        expect(result.branch).toBe('providerRejected');
        expect(result.failure?.message).not.toMatch(/SENTINEL|secret/);
      }
    }
    expect(resolves).toBe(2);
    expect(signatures).toHaveLength(2);
    expect(signatures[0]).not.toBe(signatures[1]);
  });

  it('keeps the released resolveCredentials result for a transient credential failure', async () => {
    const client = new SQSClient({ region: 'eu-west-1' }, { resolve: async () => { throw new CredentialBrokerUnavailableError(); } });
    expect(await client.resolveCredentials(call)).toBeUndefined();
  });

  it.each([405, 409, 425, 451])('retries a write answered with unlisted HTTP %i', async status => {
    const client = new SQSClient({ region: 'eu-west-1', endpoint: 'http://127.0.0.1:21096', defaultQueueUrl: queueUrl }, { resolve: async () => credentials('example-secret') }, {
      send: async () => ({ status, headers: {}, body: new TextEncoder().encode('{"__type":"Unlisted"}') }),
    });
    expect(await client.deleteMessage().invoke(call, { receiptHandle: 'receipt-1' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('selects changed when a repeated release finds the message released', async () => {
    const client = new SQSClient({ region: 'eu-west-1', endpoint: 'http://127.0.0.1:21096', defaultQueueUrl: queueUrl }, { resolve: async () => credentials('example-secret') }, {
      send: async () => ({ status: 400, headers: {}, body: new TextEncoder().encode('{"__type":"com.amazonaws.sqs#MessageNotInflight"}') }),
    });
    const repeated = { ...call, context: { ...call.context, attempt: 2 } } as Call;
    const operation = client.changeMessageVisibility();
    expect(await operation.invoke(repeated, { receiptHandle: 'receipt-1', visibilityTimeoutSeconds: 0 })).toMatchObject({ kind: 'branch', branch: 'changed' });
    expect(await operation.invoke(call, { receiptHandle: 'receipt-1', visibilityTimeoutSeconds: 0 })).toMatchObject({ kind: 'branch', branch: 'receiptHandleInvalid' });
    expect(await operation.invoke(repeated, { receiptHandle: 'receipt-1', visibilityTimeoutSeconds: 90 })).toMatchObject({ kind: 'branch', branch: 'receiptHandleInvalid' });
  });
});
