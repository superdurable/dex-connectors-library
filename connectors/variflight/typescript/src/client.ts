// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetFlightStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetFlightStatusInput, GetFlightStatusOutput, FlightStatus } from './generated/models.js';

const endpoint = 'https://open-al.variflight.com/api/flight';
const emptyOutput: GetFlightStatusOutput = { total: 0, flights: [] };
/** Send a signed VariFlight request through caller-owned transport. */
export type FlightRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional provider transport for tests. */
export interface VariFlightClientOptions { send?: FlightRequestSender }
/** Resolve partner credentials for each flight-status Query. */
export class VariFlightClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: FlightRequestSender;
  /** Validate configuration without contacting VariFlight. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: VariFlightClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('VariFlight configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('VariFlight configuration is invalid');
    this.credentials = credentials; this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the signed flight-status Query. */
  getFlightStatus(): GetFlightStatusOperation { return new GetFlightStatusOperation(this); }
}
/** Read up to twenty flight status records. */
export class GetFlightStatusOperation implements Query<GetFlightStatusInput, GetFlightStatusOutput> {
  /** Bind an authenticated client. */
  constructor(private readonly client: VariFlightClient) {}
  /** Return the generated Query definition. */
  definition(): QueryDefinition { return GetFlightStatusDefinition; }
  /** Sign, send, and classify one flight-status request. */
  async invoke(call: Call, input: GetFlightStatusInput): Promise<QueryAttempt<GetFlightStatusOutput>> {
    if (!input || typeof input.flightNumber !== 'string' || !/^[A-Z0-9]{3,8}$/.test(input.flightNumber)) return branch('defect', 'VALIDATION', 'VariFlight flight number is invalid');
    if (!validDepartureDate(input.departureDate)) return branch('defect', 'VALIDATION', 'VariFlight departure date must be YYYY-MM-DD');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'VariFlight partner credentials are unavailable or invalid'); }
    const appID = credentials.appID instanceof SecretString ? credentials.appID.reveal() : '';
    const appSecurity = credentials.appSecurity instanceof SecretString ? credentials.appSecurity.reveal() : '';
    if (!validCredential(appID) || !validCredential(appSecurity)) return branch('defect', 'AUTHENTICATION', 'VariFlight partner credentials are unavailable or invalid');
    const rawSignature = `appid=${appID}&date=${input.departureDate}&fnum=${input.flightNumber}&lang=EN${appSecurity}`;
    const first = createHash('md5').update(rawSignature).digest('hex');
    const token = createHash('md5').update(first).digest('hex');
    const query = new URLSearchParams({ appid: appID, date: input.departureDate, fnum: input.flightNumber, lang: 'EN', token });
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${endpoint}?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'VariFlight flight response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'VariFlight flight request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'VariFlight returned invalid flight data'); }
    if (isRecord(wire)) {
      if (!Number.isInteger(wire.error_code)) return branch('invalidResponse', 'PROTOCOL', 'VariFlight returned invalid error data');
      const code = wire.error_code as number;
      if (code === 10) return newQueryBranch('noData', emptyOutput);
      if (code === 500) return retry('AVAILABILITY', 'VariFlight reported temporary error');
      return branch('providerRejected', 'PROVIDER_REJECTION', `VariFlight rejected flight request with code ${code}`);
    }
    if (!Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL', 'VariFlight returned invalid flight data');
    const flights: FlightStatus[] = [];
    for (const record of wire) {
      if (!isRecord(record) || typeof record.FlightNo !== 'string' || !record.FlightNo || typeof record.FlightState !== 'string' || !record.FlightState || /[\r\n\0]/.test(record.FlightState) || !Number.isInteger(record.FlightStateNum) || typeof record.FlightDeptimePlanDate !== 'string' || record.FlightDeptimePlanDate.length > 64 || typeof record.FlightDeptimeDate !== 'string' || record.FlightDeptimeDate.length > 64) return branch('invalidResponse', 'PROTOCOL', 'VariFlight returned malformed flight status');
      if (flights.length < 20) flights.push({ flightNumber: record.FlightNo, state: record.FlightState, stateCode: record.FlightStateNum as number, scheduledDeparture: record.FlightDeptimePlanDate, actualDeparture: record.FlightDeptimeDate });
    }
    return newQueryBranch('found', { total: wire.length, flights });
  }
}
function validDepartureDate(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}
function validCredential(value: string): boolean { return !!value && value.length <= 1024 && value.trim() === value && ![...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetFlightStatusOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'VariFlight rate limited flight request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'VariFlight API is temporarily unavailable', delay);
  return branch('providerRejected', 'PROVIDER_REJECTION', `VariFlight rejected flight request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'variflight', operation: 'getFlightStatus', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetFlightStatusOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetFlightStatusOutput> { return newQueryRetry(failure(kind, message), delayMs); }
