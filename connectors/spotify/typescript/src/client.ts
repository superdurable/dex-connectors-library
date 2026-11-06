// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { ListPlaylistTracksOperation } from './operations.js';

const defaultEndpoint = 'https://api.spotify.com/v1';
const defaultResponseBytes = 4_194_304;
const defaultTimeoutMs = 15_000;

/** Replace the network transport for deterministic provider tests. */
export interface SpotifyClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** A bounded Spotify response, including status for oversized replies. */
export interface SpotifyResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; }

/** Spotify API client with host-owned renewable credentials. */
export class SpotifyClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Spotify', auth: Authorization, credentialFields: CredentialFields });

  /** Validate connection settings and bind its credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: SpotifyClientOptions = {}) {
    if (!credentials) throw new TypeError('Spotify credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultResponseBytes;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Spotify response limit is invalid');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('Spotify timeout is invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the manifest playlist query. */
  listPlaylistTracks(): ListPlaylistTracksOperation { return new ListPlaylistTracksOperation(this); }

  /** Resolve an access token through the generated manifest refresh declaration. */
  async resolveAccessToken(call: Call): Promise<string | undefined> {
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      return credential.accessToken.reveal() || undefined;
    } catch { return undefined; }
  }

  /** Request one bounded playlist page and retry once after a rejected credential if the host can refresh it. */
  async requestPage(call: Call, token: string, playlistId: string, market: string, limit: number, offset: number): Promise<SpotifyResponse> {
    const query = new URLSearchParams();
    query.set('limit', String(limit));
    if (market) query.set('market', market);
    query.set('offset', String(offset));
    const path = `/playlists/${playlistId}/items`;
    const url = `${this.endpoint}${path}?${encodeGoQuery(query)}`;
    for (let attempt = 0; attempt < 2; attempt++) {
      const request: ProviderRequest = {
        url, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'superdurable-dex-spotify-connector/0.1' },
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      };
      let response: SpotifyResponse;
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
    throw new Error('Spotify credential retry exhausted');
  }
}

/** Encode query pairs like Go url.Values.Encode, preserving repeated values. */
export function encodeGoQuery(query: URLSearchParams): string {
  const escape = (value: string) => encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
  return [...query].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(([key, value]) => `${escape(key)}=${escape(value)}`).join('&');
}
