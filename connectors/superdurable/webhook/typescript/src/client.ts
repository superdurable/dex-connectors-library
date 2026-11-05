// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newMutationBranch, newMutationRetry, type Call, type CredentialProvider, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CredentialBrokerUnavailableError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, BodyTooLargeError, parseRetryAfter, ProviderTransportError, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectorID, SendEventDefinition } from './generated/definitions.js';
import type { Config, Credentials, SendEventInput, SendEventOutput } from './generated/models.js';

/** Injectable bounded HTTP sender for provider tests. */
export type WebhookRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional dependencies for deterministic tests. */
export interface WebhookClientOptions {
  /** Replace the provider transport. */ send?: WebhookRequestSender;
  /** Replace the timestamp and receipt clock. */ now?: () => Date;
}

/** Signed webhook delivery client for one connection. */
export class WebhookClient {
  readonly deliveryUrl: string;
  readonly maxBodyBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: WebhookRequestSender;
  readonly now: () => Date;

  /** Validate immutable settings and bind a credential source. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WebhookClientOptions = {}) {
    if (!credentials) throw new TypeError('webhook credential provider is required');
    this.deliveryUrl = validateDeliveryUrl(config.deliveryUrl ?? '');
    this.maxBodyBytes = config.maxBodyBytes || 1_048_576;
    if (!Number.isSafeInteger(this.maxBodyBytes) || this.maxBodyBytes <= 0) throw new TypeError('webhook maxBodyBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the outgoing event mutation. */
  sendEvent(): SendEventOperation { return new SendEventOperation(this); }
}

/** One signed delivery attempt with a stable Standard Webhooks ID. */
export class SendEventOperation implements Mutation<SendEventInput, SendEventOutput> {
  /** Bind this operation to its client. */
  constructor(private readonly client: WebhookClient) {}
  /** Return the manifest operation definition. */
  definition(): MutationDefinition { return SendEventDefinition; }
  /** Derive the stable webhook ID from the Dex Call ID. */
  idempotencyKey(callId: string): string { return `msg_${callId.replaceAll('-', '')}`; }

  /** POST one JSON payload and classify the receiver's response. */
  async invoke(call: Call, input: SendEventInput): Promise<MutationAttempt<SendEventOutput>> {
    const webhookId = call.idempotencyKey ?? this.idempotencyKey(call.id);
    const emptyOutput: SendEventOutput = { webhookId };
    if (!this.client.deliveryUrl) return defect(emptyOutput, 'VALIDATION', 'the connection has no deliveryUrl; set it in Dex Web Connectors to the receiver\'s HTTPS webhook URL');
    let payload: Uint8Array;
    try {
      if (input.payload === undefined) throw new TypeError('missing payload');
      payload = Buffer.from(JSON.stringify(input.payload));
    } catch { return defect(emptyOutput, 'VALIDATION', 'sendEvent payload must be one valid JSON value'); }
    if (payload.length > this.client.maxBodyBytes) return defect(emptyOutput, 'VALIDATION', 'sendEvent payload exceeds the connection\'s maxBodyBytes');
    let signingSecret: string;
    try {
      const credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.signingSecret instanceof SecretString) || !credentials.signingSecret.reveal()) throw new TypeError('missing signing secret');
      signingSecret = credentials.signingSecret.reveal();
    } catch (error) {
      if (error instanceof CredentialBrokerUnavailableError) return newMutationRetry(failure('AVAILABILITY', 'the credential broker is temporarily unavailable, so nothing was sent'));
      return defect(emptyOutput, 'AUTHENTICATION', 'the webhook signing secret is unavailable');
    }
    let key: Uint8Array;
    if (signingSecret.startsWith('whsec_')) {
      const encoded = signingSecret.slice(6);
      if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded) || !encoded) {
        return defect(emptyOutput, 'VALIDATION', 'webhook signing_secret starts with whsec_ but is not valid base64');
      }
      key = Buffer.from(encoded, 'base64');
      if (!key.length) return defect(emptyOutput, 'VALIDATION', 'webhook signing_secret starts with whsec_ but is not valid base64');
    } else key = Buffer.from(signingSecret);
    const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const signed = Buffer.concat([Buffer.from(`${webhookId}.${timestamp}.`), payload]);
    const signature = createHmac('sha256', key).update(signed).digest('base64');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: this.client.deliveryUrl, method: 'POST', body: payload,
        headers: { 'Content-Type': 'application/json', 'webhook-id': webhookId, 'webhook-timestamp': timestamp, 'webhook-signature': `v1,${signature}` },
        maxResponseBytes: 64 << 10, addressPolicy: 'privateNetworkHTTP', timeoutMs: 25_000, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      } else {
        if (error instanceof AddressRefusal || error instanceof ProviderTransportError && error.isProvablyNotSent) return newMutationRetry(failure('AVAILABILITY', 'the delivery URL could not be reached'));
        return newMutationRetry(failure('TRANSPORT', 'the receiver did not answer the event'));
      }
    }
    const output: SendEventOutput = { webhookId, statusCode: response.status };
    if (response.status >= 200 && response.status < 300) return newMutationBranch('delivered', output, receipt(call, webhookId, this.client.now()));
    const retryAfterMs = parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.client.now());
    if (response.status === 408 || response.status === 429) return newMutationRetry(failure('RATE_LIMIT', `the receiver answered HTTP ${response.status}`), retryAfterMs);
    if (response.status === 409) return newMutationRetry(failure('CONFLICT', 'the receiver answered HTTP 409; retrying with the same webhook-id'), retryAfterMs);
    if (response.status === 425 || response.status >= 500) return newMutationRetry(failure('AVAILABILITY', `the receiver answered HTTP ${response.status}`), retryAfterMs);
    const kind = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 404 || response.status === 410 ? 'NOT_FOUND' : 'PROVIDER_REJECTION';
    return newMutationBranch('rejected', output, receipt(call, webhookId, this.client.now()), failure(kind, `the receiver answered HTTP ${response.status}`));
  }
}

function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: ConnectorID, operation: 'sendEvent', message }; }
function defect(output: SendEventOutput, kind: Failure['kind'], message: string): MutationAttempt<SendEventOutput> {
  return newMutationBranch('defect', output, {}, failure(kind, message));
}
function receipt(call: Call, webhookId: string, now: Date): ReceiptDraft {
  return { callId: call.id, provider: ConnectorID, providerObjectId: webhookId, observedAt: now.toISOString() };
}
function validateDeliveryUrl(value: string): string {
  if (!value) return '';
  if (!/^[!-~]+$/.test(value)) throw new TypeError('webhook deliveryUrl must be printable ASCII without spaces');
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new TypeError('webhook deliveryUrl must be an absolute HTTPS URL'); }
  if (parsed.protocol !== 'https:' || !parsed.hostname) throw new TypeError('webhook deliveryUrl must be an absolute HTTPS URL');
  if (parsed.username || parsed.password || parsed.hash) throw new TypeError('webhook deliveryUrl cannot contain user information or a fragment');
  return value;
}
