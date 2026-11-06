// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call } from '@superdurable/connectors-sdk';
import { isIP } from 'node:net';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetAuthenticatedProfileOperation } from './operations.js';

const defaultUserInfoURL = 'https://api.linkedin.com/v2/userinfo';

/** Transport override for deterministic provider tests. */
export interface LinkedInClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Bounded UserInfo response. */
export interface LinkedInResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; }

/** LinkedIn UserInfo client using host-owned renewable credentials. */
export class LinkedInClient {
  private readonly userInfoURL: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'LinkedIn', auth: Authorization, credentialFields: CredentialFields });

  /** Validate settings and bind the credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: LinkedInClientOptions = {}) {
    if (!credentials) throw new TypeError('LinkedIn credential provider is required');
    const url = new URL(config.userInfoUrl || defaultUserInfoURL);
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash)
      throw new TypeError('LinkedIn UserInfo URL is invalid');
    this.userInfoURL = url.toString();
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    this.timeoutMs = config.timeoutMs || 15_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('LinkedIn response limit is invalid');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('LinkedIn timeout is invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the authenticated profile query. */
  getAuthenticatedProfile(): GetAuthenticatedProfileOperation { return new GetAuthenticatedProfileOperation(this); }

  /** Resolve an access credential using the generated refresh declaration. */
  async resolveAccessToken(call: Call): Promise<string | undefined> {
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      return credential.accessToken.reveal() || undefined;
    } catch { return undefined; }
  }

  /** Request UserInfo and refresh once after a rejected credential if supported. */
  async requestProfile(call: Call, token: string): Promise<LinkedInResponse> {
    for (let attempt = 0; attempt < 2; attempt++) {
      const request: ProviderRequest = {
        url: this.userInfoURL, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'superdurable-dex-linkedin-connector/0.1' },
        addressPolicy: this.userInfoURL === defaultUserInfoURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      };
      let response: LinkedInResponse;
      try {
        const received = await this.send(request);
        response = { ...received, isTooLarge: false };
      } catch (error) {
        if (!(error instanceof BodyTooLargeError) || error.status === undefined || error.headers === undefined) throw error;
        response = { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
      }
      if (response.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.credentials)) return response;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        token = replacement.accessToken.reveal();
        if (!token) return response;
      } catch { return response; }
    }
    throw new Error('LinkedIn credential retry exhausted');
  }
}
