// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListSystemVoicesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListSystemVoicesInput, ListSystemVoicesOutput } from './generated/models.js';

const endpoint = 'https://maas-api.unisound.com/v1/audio/voices/query';
const emptyOutput: ListSystemVoicesOutput = { voices: [] };
/** Send a bounded request to Unisound. */
export type VoiceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned HTTP transport for provider tests. */
export interface UnisoundClientOptions { send?: VoiceRequestSender }
/** Resolve credentials for each voice-list attempt. */
export class UnisoundClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: VoiceRequestSender;
  /** Validate configuration without contacting Unisound. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: UnisoundClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Unisound configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Unisound configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the system-voice Query. */
  listSystemVoices(): ListSystemVoicesOperation { return new ListSystemVoicesOperation(this); }
}
/** Read at most 25 predefined system voices. */
export class ListSystemVoicesOperation implements Query<ListSystemVoicesInput, ListSystemVoicesOutput> {
  /** Bind an authenticated client. */
  constructor(private readonly client: UnisoundClient) {}
  /** Return the generated Query definition. */
  definition(): QueryDefinition { return ListSystemVoicesDefinition; }
  /** Send and classify one provider request. */
  async invoke(call: Call, input: ListSystemVoicesInput): Promise<QueryAttempt<ListSystemVoicesOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'Unisound voice request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Unisound API Key is unavailable or invalid'); }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 1024 || !/^[!-~]+$/.test(key)) return branch('defect', 'AUTHENTICATION', 'Unisound API Key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: endpoint, method: 'POST', headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode('{"voice_type":"system"}'), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Unisound response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'Unisound voice request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Unisound returned invalid voice data'); }
    if (!isRecord(wire) || !isRecord(wire.base_resp) || !Number.isInteger(wire.base_resp.status_code)) return branch('invalidResponse', 'PROTOCOL', 'Unisound returned invalid voice data');
    const code = wire.base_resp.status_code as number;
    if (code === 100501) return retry('RATE_LIMIT', 'Unisound rate limited the voice request');
    if (code === 100999) return retry('AVAILABILITY', 'Unisound voice service is temporarily unavailable');
    if (code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `Unisound rejected the voice request with code ${code}`);
    if (!Array.isArray(wire.system_voice) || wire.system_voice.length > 1000) return branch('invalidResponse', 'PROTOCOL', 'Unisound returned invalid system voices');
    const voices: NonNullable<ListSystemVoicesOutput['voices']> = [];
    for (const item of wire.system_voice) {
      if (voices.length === 25) break;
      if (!isRecord(item) || typeof item.voice_id !== 'string' || !item.voice_id || item.voice_id.length > 128 || item.voice_id.trim() !== item.voice_id || typeof item.voice_name !== 'string' || item.voice_name.length > 200 || !Array.isArray(item.language) || item.language.length > 20 || !item.language.every(language => typeof language === 'string')) return branch('invalidResponse', 'PROTOCOL', 'Unisound returned invalid system voice');
      voices.push({ voiceID: item.voice_id, name: item.voice_name, languages: item.language });
    }
    return newQueryBranch('found', { voices });
  }
}
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListSystemVoicesOutput> {
  const value = headers['retry-after'];
  const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Unisound rate limited the voice request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Unisound voice service is temporarily unavailable', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Unisound rejected voice request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'unisound', operation: 'listSystemVoices', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListSystemVoicesOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListSystemVoicesOutput> { return newQueryRetry(failure(kind, message), delayMs); }
