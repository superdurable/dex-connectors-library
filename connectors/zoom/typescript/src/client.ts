// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, CredentialSource } from './generated/models.js';
import { ListMeetingsOperation, GetMeetingOperation, CreateMeetingOperation, UpdateMeetingOperation, ListPastMeetingParticipantsOperation } from './operations.js';

const defaultEndpoint = 'https://api.zoom.us/v2';

/** Replace the provider transport or clock for deterministic tests. */
export interface ZoomClientOptions {
  /** Sends one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supplies receipt and Retry-After time. */ now?: () => Date;
}

/** One provider exchange, retaining whether a failed write could have reached Zoom. */
export type ZoomExchange =
  | { response: ProviderResponse; isTooLarge: boolean; reflectsCredential: boolean }
  | { error: 'beforeSend' | 'afterSend' | 'notBuilt' };

/** Zoom Meetings client with generated OAuth refresh policy. */
export class ZoomClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Zoom', auth: Authorization, credentialFields: CredentialFields });

  /** Validate connection settings before a Step resolves credentials. */
  constructor(config: Config, readonly credentials: CredentialSource, options: ZoomClientOptions = {}) {
    if (!credentials) throw new TypeError('Zoom credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Zoom maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the meeting list query. */ listMeetings() { return new ListMeetingsOperation(this); }
  /** Bind the meeting detail query. */ getMeeting() { return new GetMeetingOperation(this); }
  /** Bind the fenced meeting create mutation. */ createMeeting() { return new CreateMeetingOperation(this); }
  /** Bind the meeting patch mutation. */ updateMeeting() { return new UpdateMeetingOperation(this); }
  /** Bind the participant list query. */ listPastMeetingParticipants() { return new ListPastMeetingParticipantsOperation(this); }

  /** Resolve generated renewable credentials and preserve terminal versus retryable failures. */
  async resolveCredentials(call: Call, operation: string): Promise<{ credentials: Credentials } | { failure: Failure; retry: boolean }> {
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!credentials.accessToken || !isHeaderSafeCredential(credentials.accessToken.reveal()))
        return { failure: zoomFailure(operation, 'AUTHENTICATION', 'Zoom connection credentials are unavailable; reconnect the connection'), retry: false };
      return { credentials };
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { failure: zoomFailure(operation, 'AUTHENTICATION', 'Zoom connection requires reauthorization; reconnect the connection'), retry: false };
      return { failure: zoomFailure(operation, 'AVAILABILITY', 'Zoom connection credentials could not be refreshed'), retry: true };
    }
  }

  /** Force one generated refresh after Zoom rejects a fenced create credential. */
  async refreshAfterRejection(call: Call, previousAccessToken: string): Promise<'replaced' | 'retryable' | 'reauthorize' | 'unsupported'> {
    if (!('resolveAfterRejection' in this.credentials)) return 'unsupported';
    try {
      const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      return replacement.accessToken?.reveal() && replacement.accessToken.reveal() !== previousAccessToken ? 'replaced' : 'reauthorize';
    } catch (error) { return error instanceof ReauthorizationRequiredError ? 'reauthorize' : 'retryable'; }
  }

  /** Send one bounded request and refresh once after a rejected credential when allowed. */
  async exchange(call: Call, credentials: Credentials, method: string, path: string, query?: URLSearchParams, payload?: unknown, isFencedWrite = false, signal?: AbortSignal): Promise<ZoomExchange> {
    const sorted = query ? [...query.entries()].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right))) : [];
    const queryString = new URLSearchParams(sorted).toString();
    const url = `${this.endpoint}${path}${queryString ? `?${queryString}` : ''}`;
    for (let attempt = 0; attempt < 2; attempt++) {
      const accessToken = credentials.accessToken.reveal();
      const request: ProviderRequest = {
        url, method,
        headers: { Authorization: `Bearer ${accessToken}`, Accept: 'application/json', ...(payload === undefined ? {} : { 'Content-Type': 'application/json' }) },
        ...(payload === undefined ? {} : { body: Buffer.from(JSON.stringify(payload), 'utf8') }),
        maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), maxErrorResponseBytes: MaxErrorBodyBytes,
        ignoreErrorBodyReadFailure: true, timeoutMs: 20_000,
        addressPolicy: 'unrestricted',
        signal: signal ?? call.context.cancellationSignal,
      };
      let result: ZoomExchange;
      try {
        const response = await this.send(request);
        const isTooLarge = response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes;
        result = { response: { ...response, body: isTooLarge ? new Uint8Array() : response.body }, isTooLarge,
          reflectsCredential: !isTooLarge && accessToken !== '' && Buffer.from(response.body).includes(Buffer.from(accessToken)) };
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) result = { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, isTooLarge: error.status >= 200 && error.status < 300, reflectsCredential: false };
        else if (error instanceof ProviderTransportError) return { error: error.isProvablyNotSent ? 'beforeSend' : 'afterSend' };
        else if (error instanceof AddressRefusal || error instanceof TypeError) return { error: 'notBuilt' };
        else return { error: 'afterSend' };
      }
      if (result.response.status !== 401 || attempt !== 0 || isFencedWrite || !('resolveAfterRejection' in this.credentials)) return result;
      try {
        credentials = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        if (!credentials.accessToken?.reveal()) return result;
      } catch { return result; }
    }
    return { error: 'afterSend' };
  }

  /** Build a secret-free receipt containing only the provider meeting ID and numeric error code. */
  receipt(call: Call, objectId = '', errorCode = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'zoom',
      ...(objectId ? { providerObjectId: objectId } : {}), observedAt: this.now().toISOString(),
      ...(errorCode ? { metadata: { zoomErrorCode: errorCode } } : {}) };
  }

  /** Parse Retry-After with the SDK's bounded rules. */
  retryAfter(headers: ProviderResponse['headers']): number {
    const value = headers['retry-after'];
    return parseRetryAfter(typeof value === 'string' ? value : '', this.now());
  }
}

/** Construct one secret-free Zoom failure. */
export function zoomFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'zoom', operation, message };
}
