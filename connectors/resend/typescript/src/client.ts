// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { isCredentialMaterialError, type Config, type Credentials } from './generated/models.js';
import { GetEmailOperation, SendEmailOperation } from './operations.js';

const publicEndpoint = 'https://api.resend.com';

/** Provider sender, injectable for deterministic tests. */
export type ResendRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** HTTP dependencies supplied at construction. */
export interface ResendClientOptions {
  /** Override the transport for tests and compatible gateways. */
  send?: ResendRequestSender;
}

/** One bounded Resend response, including a body-size classification. */
export interface ResendResponse extends ProviderResponse {
  /** True when a successful response exceeded the configured byte limit. */
  isTooLarge: boolean;
}

/** Resend client that resolves current credentials for every operation attempt. */
export class ResendClient {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: ResendRequestSender;

  /** Validate local configuration without calling Resend. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ResendClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Resend configuration and credentials are required');
    this.endpoint = validateBaseURL(config.baseUrl || publicEndpoint);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) {
      throw new TypeError('Resend timeout and response limit must be nonnegative integers');
    }
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the keyed send mutation. */
  sendEmail(): SendEmailOperation { return new SendEmailOperation(this); }
  /** Bind the email lookup query. */
  getEmail(): GetEmailOperation { return new GetEmailOperation(this); }

  /** Resolve a safe API key; invalid and unavailable credentials are terminal. */
  async resolveAPIKey(call: Call): Promise<string | undefined> {
    return (await this.resolveAPIKeyResolution(call)).apiKey;
  }

  /**
   * Resolve a safe API key, or report whether the failure may clear on a later attempt. Only reauthorization,
   * a missing or unselected connection, a broker refusal, and unusable material are terminal.
   */
  async resolveAPIKeyResolution(call: Call): Promise<{ apiKey?: string; isTransient: boolean }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch (error) { return { isTransient: !isTerminalCredentialError(error) }; }
    if (!(credentials?.apiKey instanceof SecretString)) return { isTransient: false };
    const key = credentials.apiKey.reveal();
    return key.trim() && isHeaderSafeCredential(key) ? { apiKey: key, isTransient: false } : { isTransient: false };
  }

  /** Send one no-redirect request with Resend's authentication and content headers. */
  async exchange(call: Call, apiKey: string, method: 'GET' | 'POST', path: string, body?: Uint8Array): Promise<ResendResponse> {
    const headers: Record<string, string> = { Authorization: `Bearer ${apiKey}`, Accept: 'application/json', 'User-Agent': 'superdurable-resend/0.1' };
    if (body) headers['Content-Type'] = 'application/json';
    if (method === 'POST') headers['Idempotency-Key'] = call.idempotencyKey ?? '';
    const request: ProviderRequest = {
      url: this.endpoint + path, method, headers, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint === publicEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    try { return { ...await this.send(request), isTooLarge: false }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
      }
      throw error;
    }
  }
}

/** Report a resolve failure no later attempt clears; a ZodError is stored material the decoder rejected. */
function isTerminalCredentialError(error: unknown): boolean {
  return error instanceof ReauthorizationRequiredError || error instanceof ObjectNotFoundError
    || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError
    || error instanceof z.ZodError || isCredentialMaterialError(error);
}
