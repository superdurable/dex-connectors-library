// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt } from '@superdurable/connectors-sdk';
import { ChangeMessageVisibilityDefinition, DeleteMessageDefinition, ReceiveMessagesDefinition } from './generated/definitions.js';
import { ChangeMessageVisibilityInputSchema, DeleteMessageInputSchema, ReceiveMessagesInputSchema, type ChangedMessageVisibility, type ChangeMessageVisibilityInput, type DeletedMessage, type DeleteMessageInput, type Message, type ReceivedMessages, type ReceiveMessagesInput } from './generated/models.js';
import { classifyResponse, receiveSystemAttributes, type QueueAddress, type SQSClient, type SQSResponse } from './client.js';

const provider = 'amazon-sqs';
const printable = /^[\x21-\x7e]+$/;
const messageAttributeName = /^[A-Za-z0-9_.-]{1,256}$/;
const zeroReceived: ReceivedMessages = { queueUrl: '' };
const zeroDeleted: DeletedMessage = { queueUrl: '' };
const zeroChanged: ChangedMessageVisibility = { queueUrl: '', visibilityTimeoutSeconds: 0, visibleAfter: '0001-01-01T00:00:00Z' };

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider, operation, kind, message }; }
function defect<Output>(operation: string, output: Output, message: string): MutationAttempt<Output> {
  return newMutationBranch('defect', output, zeroReceipt(), failure(operation, 'VALIDATION', message));
}
function credentialsDefect<Output>(operation: string, output: Output): MutationAttempt<Output> {
  return newMutationBranch('defect', output, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'SQS connection credentials are unavailable; save an access key ID and secret access key for the connection'));
}
function transportRetry<Output>(operation: string, subject: string): MutationAttempt<Output> {
  return newMutationRetry(failure(operation, 'TRANSPORT', `SQS could not be reached or did not finish the ${subject}`));
}
function validReceiptHandle(handle: string): boolean { return !!handle && Buffer.byteLength(handle) <= 4096 && printable.test(handle); }
function validMessageId(id: string): boolean { return Buffer.byteLength(id) <= 128 && (!id || printable.test(id)); }
function acknowledgementInput(client: SQSClient, queueUrl: string | undefined, handle: string, messageId: string | undefined): QueueAddress {
  const queue = client.resolveQueue(queueUrl);
  if (!validReceiptHandle(handle)) throw new TypeError('receiptHandle must be the handle receiveMessages returned for the message');
  if (!validMessageId(messageId ?? '')) throw new TypeError('messageId must be blank or the message ID receiveMessages returned');
  return queue;
}
function classifyAcknowledgement<Output>(call: Call, client: SQSClient, response: SQSResponse, credentials: NonNullable<Awaited<ReturnType<SQSClient['resolveCredentials']>>>, operation: string, subject: string, output: Output, objectId: string): MutationAttempt<Output> | undefined {
  if (response.status >= 200 && response.status < 300) return undefined;
  const classified = classifyResponse(operation, subject, response, credentials, client.now());
  if (classified.branch === 'retry') return newMutationRetry(classified.failure, classified.retryAfterMs);
  return newMutationBranch(classified.branch, output, client.receipt(call, response, objectId), classified.failure);
}

/** Receive a verified message batch from a standard or FIFO queue. */
export class ReceiveMessagesOperation implements Mutation<ReceiveMessagesInput, ReceivedMessages> {
  /** Bind one SQS client. */ constructor(private readonly client: SQSClient) {}
  /** Return the manifest operation identity. */ definition() { return ReceiveMessagesDefinition; }
  /** Reuse the call ID for FIFO receive deduplication. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, sign, send, and classify one receive attempt. */
  async invoke(call: Call, input: ReceiveMessagesInput): Promise<MutationAttempt<ReceivedMessages>> {
    const operation = 'receiveMessages';
    if (!ReceiveMessagesInputSchema.safeParse(input).success) return defect(operation, zeroReceived, 'SQS receive input is invalid');
    let queue: QueueAddress;
    try { queue = this.client.resolveQueue(input.queueUrl); }
    catch (error) { return defect(operation, zeroReceived, (error as Error).message); }
    const max = input.maxMessages || this.client.maxMessagesPerReceive;
    if (!Number.isSafeInteger(max) || max < 1 || max > 10) return defect(operation, zeroReceived, "maxMessages must be from 1 to 10, or zero for the connection's maxMessagesPerReceive");
    const wait = input.waitTimeSeconds ?? this.client.receiveWaitSeconds;
    if (!Number.isSafeInteger(wait) || wait < 0 || wait > 20) return defect(operation, zeroReceived, 'waitTimeSeconds must be from 0 to 20');
    const visibility = input.visibilityTimeoutSeconds;
    if (visibility !== undefined && (!Number.isSafeInteger(visibility) || visibility < 0 || visibility > 43200)) return defect(operation, zeroReceived, 'visibilityTimeoutSeconds must be from 0 to 43200');
    const attemptId = call.idempotencyKey ?? '';
    if (queue.isFIFO && (!attemptId || Buffer.byteLength(attemptId) > 128 || !printable.test(attemptId))) return defect(operation, zeroReceived, 'idempotency key is missing or not a valid ReceiveRequestAttemptId');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return credentialsDefect(operation, zeroReceived);
    const batch: ReceivedMessages = { queueUrl: queue.url, ...(queue.isFIFO ? { isFifo: true } : {}) };
    const attributes = receiveSystemAttributes();
    const request = { QueueUrl: queue.url, MaxNumberOfMessages: max, WaitTimeSeconds: wait,
      ...(visibility !== undefined ? { VisibilityTimeout: visibility } : {}),
      AttributeNames: attributes, MessageSystemAttributeNames: attributes, MessageAttributeNames: ['All'],
      ...(queue.isFIFO ? { ReceiveRequestAttemptId: attemptId } : {}) };
    let response: SQSResponse;
    try { response = await this.client.exchange(call, credentials, 'ReceiveMessage', request); }
    catch { return transportRetry(operation, 'receive'); }
    if (response.status < 200 || response.status >= 300) {
      const classified = classifyResponse(operation, 'receive', response, credentials, this.client.now());
      if (classified.branch === 'retry') return newMutationRetry(classified.failure, classified.retryAfterMs);
      return newMutationBranch('providerRejected', batch, this.client.receipt(call, response, queue.url), classified.failure);
    }
    const receipt = this.client.receipt(call, response, queue.url);
    if (response.isTooLarge) return newMutationBranch('invalidResponse', batch, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'SQS receive batch exceeds maxResponseBytes; lower maxMessages'));
    const body = Buffer.from(response.body).toString('utf8');
    if ([credentials.secretAccessKey.reveal(), credentials.sessionToken?.reveal()].some(secret => secret && body.includes(secret))) return newMutationBranch('invalidResponse', batch, receipt, failure(operation, 'PROTOCOL', "SQS receive batch contains the connection's credentials"));
    try {
      const messages = decodeMessages(body, max);
      if (messages.length) batch.messages = messages;
      return newMutationBranch(messages.length ? 'received' : 'empty', batch, receipt);
    } catch (error) {
      return newMutationBranch('invalidResponse', batch, receipt, failure(operation, 'PROTOCOL', `SQS returned an invalid receive batch: ${(error as Error).message}`));
    }
  }
}

/** Acknowledge one received SQS delivery by receipt handle. */
export class DeleteMessageOperation implements Mutation<DeleteMessageInput, DeletedMessage> {
  /** Bind one SQS client. */ constructor(private readonly client: SQSClient) {}
  /** Return the manifest operation identity. */ definition() { return DeleteMessageDefinition; }
  /** Stable mutation key for Dex retries. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, sign, send, and classify one delete attempt. */
  async invoke(call: Call, input: DeleteMessageInput): Promise<MutationAttempt<DeletedMessage>> {
    const operation = 'deleteMessage';
    if (!DeleteMessageInputSchema.safeParse(input).success) return defect(operation, zeroDeleted, 'SQS delete input is invalid');
    let queue: QueueAddress;
    try { queue = acknowledgementInput(this.client, input.queueUrl, input.receiptHandle, input.messageId); }
    catch (error) { return defect(operation, zeroDeleted, (error as Error).message); }
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return credentialsDefect(operation, zeroDeleted);
    const deleted: DeletedMessage = { queueUrl: queue.url, ...(input.messageId ? { messageId: input.messageId } : {}) };
    const objectId = input.messageId ? `${queue.url}#${input.messageId}` : queue.url;
    let response: SQSResponse;
    try { response = await this.client.exchange(call, credentials, 'DeleteMessage', { QueueUrl: queue.url, ReceiptHandle: input.receiptHandle }); }
    catch { return transportRetry(operation, 'message deletion'); }
    const error = classifyAcknowledgement(call, this.client, response, credentials, operation, 'message deletion', deleted, objectId);
    return error ?? newMutationBranch('deleted', deleted, this.client.receipt(call, response, objectId));
  }
}

/** Release or extend one received SQS delivery. */
export class ChangeMessageVisibilityOperation implements Mutation<ChangeMessageVisibilityInput, ChangedMessageVisibility> {
  /** Bind one SQS client. */ constructor(private readonly client: SQSClient) {}
  /** Return the manifest operation identity. */ definition() { return ChangeMessageVisibilityDefinition; }
  /** Stable mutation key for Dex retries. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, sign, send, and classify one visibility attempt. */
  async invoke(call: Call, input: ChangeMessageVisibilityInput): Promise<MutationAttempt<ChangedMessageVisibility>> {
    const operation = 'changeMessageVisibility';
    if (!ChangeMessageVisibilityInputSchema.safeParse(input).success) return defect(operation, zeroChanged, 'SQS visibility input is invalid');
    let queue: QueueAddress;
    try { queue = acknowledgementInput(this.client, input.queueUrl, input.receiptHandle, input.messageId); }
    catch (error) { return defect(operation, zeroChanged, (error as Error).message); }
    if (!Number.isSafeInteger(input.visibilityTimeoutSeconds) || input.visibilityTimeoutSeconds < 0 || input.visibilityTimeoutSeconds > 43200) return defect(operation, zeroChanged, 'visibilityTimeoutSeconds must be from 0 to 43200');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return credentialsDefect(operation, zeroChanged);
    const changed: ChangedMessageVisibility = { queueUrl: queue.url, ...(input.messageId ? { messageId: input.messageId } : {}), visibilityTimeoutSeconds: input.visibilityTimeoutSeconds, visibleAfter: '0001-01-01T00:00:00Z' };
    const objectId = input.messageId ? `${queue.url}#${input.messageId}` : queue.url;
    let response: SQSResponse;
    try { response = await this.client.exchange(call, credentials, 'ChangeMessageVisibility', { QueueUrl: queue.url, ReceiptHandle: input.receiptHandle, VisibilityTimeout: input.visibilityTimeoutSeconds }); }
    catch { return transportRetry(operation, 'visibility change'); }
    const classified = classifyAcknowledgement(call, this.client, response, credentials, operation, 'visibility change', changed, objectId);
    if (classified) return classified;
    const receipt = this.client.receipt(call, response, objectId);
    changed.visibleAfter = new Date(Date.parse(receipt.observedAt) + input.visibilityTimeoutSeconds * 1000).toISOString().replace(/\.000Z$/, 'Z');
    return newMutationBranch('changed', changed, receipt);
  }
}

function decodeMessages(body: string, max: number): Message[] {
  let parsed: unknown;
  try { parsed = JSON.parse(body); } catch { throw new TypeError('the document is not a ReceiveMessage response'); }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new TypeError('the document is not a ReceiveMessage response');
  const raw = (parsed as { Messages?: unknown }).Messages;
  if (raw !== undefined && raw !== null && !Array.isArray(raw)) throw new TypeError('the document is not a ReceiveMessage response');
  const entries = (raw ?? []) as unknown[];
  if (entries.length > max) throw new TypeError('the batch holds more messages than requested');
  return entries.map(decodeMessage);
}
function decodeMessage(value: unknown): Message {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('a message ID is missing or not printable');
  const item = value as Record<string, unknown>;
  const id = item.MessageId;
  const handle = item.ReceiptHandle;
  const body = item.Body;
  if (typeof id !== 'string' || !validMessageId(id) || !id) throw new TypeError('a message ID is missing or not printable');
  if (typeof handle !== 'string' || !validReceiptHandle(handle)) throw new TypeError('a receipt handle is missing or not printable');
  if (typeof body !== 'string') throw new TypeError('a message body is missing or not UTF-8');
  const digest = createHash('md5').update(body).digest('hex');
  if (item.MD5OfBody !== digest) throw new TypeError('a message body does not match its MD5 digest');
  const attributes = item.Attributes && typeof item.Attributes === 'object' ? item.Attributes as Record<string, unknown> : {};
  const count = Number(attributes.ApproximateReceiveCount);
  if (!Number.isSafeInteger(count) || count < 1) throw new TypeError('a receive count is not a positive integer');
  const timestamp = (name: string): string | undefined => {
    const raw = attributes[name];
    if (raw === undefined || raw === '') return undefined;
    if (typeof raw !== 'string' || !/^\d+$/.test(raw) || !Number.isSafeInteger(Number(raw))) throw new TypeError('timestamp is not epoch milliseconds');
    return new Date(Number(raw)).toISOString().replace(/\.000Z$/, 'Z');
  };
  const message: Message = { messageId: id, receiptHandle: handle, body, md5OfBody: digest, receiveCount: count,
    ...(timestamp('SentTimestamp') ? { sentAt: timestamp('SentTimestamp') } : {}),
    ...(timestamp('ApproximateFirstReceiveTimestamp') ? { firstReceivedAt: timestamp('ApproximateFirstReceiveTimestamp') } : {}),
    ...(typeof attributes.MessageGroupId === 'string' && attributes.MessageGroupId ? { messageGroupId: attributes.MessageGroupId } : {}),
    ...(typeof attributes.MessageDeduplicationId === 'string' && attributes.MessageDeduplicationId ? { messageDeduplicationId: attributes.MessageDeduplicationId } : {}),
    ...(typeof attributes.SequenceNumber === 'string' && attributes.SequenceNumber ? { sequenceNumber: attributes.SequenceNumber } : {}) };
  const rawMessageAttributes = item.MessageAttributes;
  if (rawMessageAttributes && typeof rawMessageAttributes === 'object' && !Array.isArray(rawMessageAttributes)) {
    const decoded: NonNullable<Message['messageAttributes']> = {};
    for (const [name, rawValue] of Object.entries(rawMessageAttributes)) {
      const attribute = rawValue as Record<string, unknown>;
      if (!messageAttributeName.test(name) || typeof attribute.DataType !== 'string' || !attribute.DataType || (attribute.StringValue !== undefined && typeof attribute.StringValue !== 'string')) throw new TypeError('a message attribute has an invalid name, data type, or value');
      if (attribute.BinaryValue !== undefined && (typeof attribute.BinaryValue !== 'string' || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(attribute.BinaryValue))) throw new TypeError('a message attribute has an invalid name, data type, or value');
      decoded[name] = { dataType: attribute.DataType, ...(attribute.StringValue ? { stringValue: attribute.StringValue } : {}),
        ...(attribute.BinaryValue ? { binaryValue: attribute.BinaryValue as string } : {}) };
    }
    if (Object.keys(decoded).length) message.messageAttributes = decoded;
  }
  return message;
}
