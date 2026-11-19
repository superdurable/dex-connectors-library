// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { type Call } from '@superdurable/connectors-sdk';
import { isIP } from 'node:net';
import type { Config, CredentialSource, GeocodeAddressInput } from './generated/models.js';
import { GeocodeAddressOperation } from './operations.js';

/** Override the sender for deterministic provider tests. */
export interface BaiduMapsClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Bounded geocoding response. */
export interface GeocodeResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; }

/** Baidu Maps geocoding client with host-owned credentials. */
export class BaiduMapsClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate request settings and bind the credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: BaiduMapsClientOptions = {}) {
    if (!credentials) throw new TypeError('Baidu Maps credential provider is required');
    const url = new URL(config.baseUrl || 'https://api.map.baidu.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || (url.pathname !== '/' && url.pathname !== ''))
      throw new TypeError('Baidu Maps base URL is invalid');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Baidu Maps timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Baidu Maps response limit is invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind one address query. */
  geocodeAddress(): GeocodeAddressOperation { return new GeocodeAddressOperation(this); }

  /** Resolve the server AK without placing it in Flow state. */
  async resolveAccessKey(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call);
      const key = credentials.accessKey.reveal();
      return key.length > 0 && key.length <= 256 && /^[\x21-\x7e]+$/.test(key) ? key : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded geocoding request. */
  async requestGeocode(call: Call, accessKey: string, input: GeocodeAddressInput): Promise<GeocodeResponse> {
    const url = new URL('/geocoding/v3/', this.baseURL);
    url.searchParams.set('ak', accessKey);
    url.searchParams.set('address', input.address);
    if (input.city) url.searchParams.set('city', input.city);
    url.searchParams.set('output', 'json');
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET', headers: { Accept: 'application/json' },
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
    };
    try { return { ...await this.send(request), isTooLarge: false }; }
    catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined || error.headers === undefined) throw error;
      return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
    }
  }
}
