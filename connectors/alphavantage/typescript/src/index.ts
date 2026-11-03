// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { SecretString, defaultConnectorStepExecuteRetryPolicy, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { Connection, newConnection, newConnectionWithOperations, newProjectConnection as generatedProjectConnection, type Operations } from './generated/connection.js';
import { newGetGlobalQuoteStep, newGetTimeSeriesDailyStep } from './generated/steps.js';
import { ConnectorID, GetGlobalQuoteDefinition as GeneratedGlobalQuoteDefinition, GetTimeSeriesDailyDefinition as GeneratedTimeSeriesDailyDefinition } from './generated/definitions.js';
import type { Config, Credentials, DailyPrice, DailySeriesOutput, GlobalQuoteOutput, StockInput } from './generated/models.js';

export { Connection, newConnection, newConnectionWithOperations, newGetGlobalQuoteStep, newGetTimeSeriesDailyStep };
export * from './generated/definitions.js';
export { StockInputSchema, GlobalQuoteOutputSchema, DailyPriceSchema, DailySeriesOutputSchema, ConfigSchema } from './generated/models.js';
export type { Config, Credentials, StockInput, GlobalQuoteOutput, DailyPrice, DailySeriesOutput, Operations };

/** Quote contract with the Dex connector retry defaults. */
export const GetGlobalQuoteDefinition: QueryDefinition = { ...GeneratedGlobalQuoteDefinition, stepDefaults: { ...GeneratedGlobalQuoteDefinition.stepDefaults, executeRetry: defaultConnectorStepExecuteRetryPolicy() } };
/** Daily series contract with the Dex connector retry defaults. */
export const GetTimeSeriesDailyDefinition: QueryDefinition = { ...GeneratedTimeSeriesDailyDefinition, stepDefaults: { ...GeneratedTimeSeriesDailyDefinition.stepDefaults, executeRetry: defaultConnectorStepExecuteRetryPolicy() } };

const emptyQuote: GlobalQuoteOutput = { symbol: '', price: '', latestTradingDay: '' };
const emptyDailySeries: DailySeriesOutput = { symbol: '', days: [] };
const symbolPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,24}$/;

type Branches = { found: string; notFound: string; providerRejected: string; invalidResponse: string; defect: string };
const quoteBranches: Branches = { found: 'found', notFound: 'notFound', providerRejected: 'providerRejected', invalidResponse: 'invalidResponse', defect: 'defect' };
const dailyBranches: Branches = quoteBranches;

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { provider: ConnectorID, operation, kind, message };
}

function branch<Output>(call: Call, operation: string, branchId: string, value: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branchId, value, { callId: call.id, provider: ConnectorID, observedAt: new Date().toISOString() }, failure(operation, kind, message));
}

function found<Output>(call: Call, value: Output): QueryAttempt<Output> {
  return newQueryBranch('found', value, { callId: call.id, provider: ConnectorID, observedAt: new Date().toISOString() });
}

class NoDataError extends Error {}

/** Immutable provider client resolving credentials for each Dex call. */
export class Client implements Operations {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly quoteOperation: GetGlobalQuoteOperation;
  private readonly dailyOperation: GetTimeSeriesDailyOperation;

  /** Validate local configuration without contacting Alpha Vantage. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, private readonly send: (request: ProviderRequest) => Promise<ProviderResponse> = sendProviderRequest) {
    if (!credentials) throw new TypeError('Alpha Vantage credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://www.alphavantage.co');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('timeoutMs must be positive milliseconds');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('maxResponseBytes must be positive bytes');
    this.quoteOperation = new GetGlobalQuoteOperation(this);
    this.dailyOperation = new GetTimeSeriesDailyOperation(this);
  }

  /** Return the global quote query. */ getGlobalQuote(): GetGlobalQuoteOperation { return this.quoteOperation; }
  /** Return the compact daily series query. */ getTimeSeriesDaily(): GetTimeSeriesDailyOperation { return this.dailyOperation; }

  /** Execute one safe read and classify its provider response. */
  async invokeJSONQuery<Output>(call: Call, input: StockInput, operation: string, functionName: string, branches: Branches, emptyValue: Output, decode: (body: unknown, symbol: string) => Output): Promise<QueryAttempt<Output>> {
    if (!symbolPattern.test(input.symbol)) return branch(call, operation, branches.defect, emptyValue, 'VALIDATION', 'symbol is invalid');
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch(call, operation, branches.defect, emptyValue, 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    if (!(credentials.apiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.apiKey.reveal())) {
      return branch(call, operation, branches.defect, emptyValue, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    }
    const query = new URLSearchParams({ apikey: credentials.apiKey.reveal(), function: functionName, symbol: input.symbol });
    if (functionName === 'TIME_SERIES_DAILY') query.set('outputsize', 'compact');
    let response;
    try {
      response = await this.send({ url: `${this.baseUrl}/query?${query.toString()}`, method: 'GET', headers: { Accept: 'application/json' }, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 200;
        if (status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the request'), parseRetryAfter(error.headers?.['retry-after'] ?? ''));
        if (status === 408 || status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is temporarily unavailable'), parseRetryAfter(error.headers?.['retry-after'] ?? ''));
        if (status === 404) return branch(call, operation, branches.notFound, emptyValue, 'NOT_FOUND', 'provider did not find the resource');
        if (status !== 200) return branch(call, operation, branches.providerRejected, emptyValue, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`);
        return branch(call, operation, branches.invalidResponse, emptyValue, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      }
      if (error instanceof AddressRefusal) return branch(call, operation, branches.defect, emptyValue, 'LOCAL_DEFECT', 'provider request could not be built');
      if (error instanceof ProviderTransportError) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed'));
      return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed'));
    }
    const retryAfter = parseRetryAfter(response.headers['retry-after'] ?? '');
    if (response.status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the request'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is temporarily unavailable'), retryAfter);
    if (response.status === 404) return branch(call, operation, branches.notFound, emptyValue, 'NOT_FOUND', 'provider did not find the resource');
    if (response.status !== 200) return branch(call, operation, branches.providerRejected, emptyValue, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return branch(call, operation, branches.invalidResponse, emptyValue, 'PROTOCOL', 'provider returned an invalid response'); }
    if (document && typeof document === 'object' && !Array.isArray(document)) {
      const signal = document as Record<string, unknown>;
      if (Object.hasOwn(signal, 'Note')) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'Alpha Vantage rate limit notice'));
      if (Object.hasOwn(signal, 'Information')) {
        const information = typeof signal.Information === 'string' ? signal.Information.toLowerCase() : '';
        if (information.includes('rate limit') || information.includes('call frequency') || information.includes('requests per day')) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'Alpha Vantage rate limit notice'));
        return branch(call, operation, branches.providerRejected, emptyValue, 'PROVIDER_REJECTION', 'Alpha Vantage rejected the request');
      }
      if (Object.hasOwn(signal, 'Error Message')) return branch(call, operation, branches.providerRejected, emptyValue, 'PROVIDER_REJECTION', 'Alpha Vantage rejected the request');
    }
    try { return found(call, decode(document, input.symbol)); }
    catch (error) {
      if (error instanceof NoDataError) return branch(call, operation, branches.notFound, emptyValue, 'NOT_FOUND', 'Alpha Vantage returned no data for the symbol');
      return branch(call, operation, branches.invalidResponse, emptyValue, 'PROTOCOL', 'provider returned an invalid response');
    }
  }
}

/** Read the latest quote for one stock symbol. */
export class GetGlobalQuoteOperation implements Query<StockInput, GlobalQuoteOutput> {
  /** Bind the provider client. */ constructor(private readonly client: Client) {}
  /** Return the stable operation definition. */ definition(): QueryDefinition { return GetGlobalQuoteDefinition; }
  /** Invoke the Alpha Vantage GLOBAL_QUOTE endpoint once. */
  invoke(call: Call, input: StockInput): Promise<QueryAttempt<GlobalQuoteOutput>> {
    return this.client.invokeJSONQuery(call, input, 'getGlobalQuote', 'GLOBAL_QUOTE', quoteBranches, emptyQuote, decodeGlobalQuote);
  }
}

/** Read compact daily prices for one stock symbol. */
export class GetTimeSeriesDailyOperation implements Query<StockInput, DailySeriesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: Client) {}
  /** Return the stable operation definition. */ definition(): QueryDefinition { return GetTimeSeriesDailyDefinition; }
  /** Invoke the Alpha Vantage TIME_SERIES_DAILY endpoint once. */
  invoke(call: Call, input: StockInput): Promise<QueryAttempt<DailySeriesOutput>> {
    return this.client.invokeJSONQuery(call, input, 'getTimeSeriesDaily', 'TIME_SERIES_DAILY', dailyBranches, emptyDailySeries, decodeDailySeries);
  }
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected JSON object');
  return value as Record<string, unknown>;
}

function wireString(value: unknown): string { return typeof value === 'string' ? value : ''; }

function decodeGlobalQuote(body: unknown): GlobalQuoteOutput {
  const quote = record(record(body ?? {})['Global Quote'] ?? {});
  const output = { symbol: wireString(quote['01. symbol']), price: wireString(quote['05. price']), latestTradingDay: wireString(quote['07. latest trading day']) };
  if (!output.symbol && !output.price && !output.latestTradingDay) throw new NoDataError();
  if (!output.symbol || !output.price || !output.latestTradingDay) throw new TypeError('quote fields missing');
  return output;
}

function decodeDailySeries(body: unknown, symbol: string): DailySeriesOutput {
  const series = record(record(body ?? {})['Time Series (Daily)'] ?? {});
  if (Object.keys(series).length === 0) throw new NoDataError();
  const days: DailyPrice[] = Object.entries(series).map(([date, value]) => {
    const point = record(value);
    const day = { date, open: wireString(point['1. open']), high: wireString(point['2. high']), low: wireString(point['3. low']), close: wireString(point['4. close']), volume: wireString(point['5. volume']) };
    if (!day.open || !day.high || !day.low || !day.close || !day.volume) throw new TypeError('daily point incomplete');
    return day;
  });
  days.sort((left, right) => right.date.localeCompare(left.date));
  return { symbol, days };
}

/** Open a named Dex Web connection and resolve its secret for each request. */
export function newProjectConnection(project: LoadedProject, connectionName: string): Connection {
  return generatedProjectConnection(project, connectionName, (loadedProject, name) => {
    const selected = loadedProject.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === name);
    if (!selected) throw new TypeError('Alpha Vantage project connection is not configured');
    const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(selected.configuration);
    const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
    const credentials: CredentialProvider<Credentials> = { async resolve(_call, signal) {
      const privateFields = await loadedProject.resolveCredential(ConnectorID, name, signal);
      if (typeof privateFields.api_key !== 'string') throw new TypeError('Alpha Vantage API key is unavailable');
      return { apiKey: new SecretString(privateFields.api_key) };
    } };
    return new Client({ ...(fields.baseUrl !== undefined ? { baseUrl: fields.baseUrl } : {}), ...(timeoutMs !== undefined ? { timeoutMs } : {}), ...(fields.maxResponseBytes !== undefined ? { maxResponseBytes: fields.maxResponseBytes } : {}) }, credentials);
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let total = 0;
  const units: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('Alpha Vantage timeout must be a positive Go duration');
    total += Number(component[1]) * (units[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(total) || total <= 0) throw new TypeError('Alpha Vantage timeout must resolve to positive whole milliseconds');
  return total;
}
