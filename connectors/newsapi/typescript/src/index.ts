// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import type { StepTarget } from '@superdurable/dex';
import { SecretString, newQueryStep, type Call, type ConnectionRef, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition, type QueryResult, type QueryStep, type QueryStepConfig, newQueryBranch, newQueryRetry, defaultConnectorStepExecuteRetryPolicy } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL } from '@superdurable/connectors-sdk/providerhttp';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

/** Stable connector identity. */
export const ConnectorID = 'newsapi';
/** Successful article search branch. */
export const SearchArticlesBranchSearched = 'searched';
/** Conclusive provider rejection branch. */
export const SearchArticlesBranchProviderRejected = 'providerRejected';
/** Malformed or oversized provider response branch. */
export const SearchArticlesBranchInvalidResponse = 'invalidResponse';
/** Local validation or credential defect branch. */
export const SearchArticlesBranchDefect = 'defect';

/** Supported NewsAPI article fields. */
export const SearchInFields = ['title', 'description', 'content'] as const;
/** Supported two-letter language codes. */
export const Languages = ['ar', 'de', 'en', 'es', 'fr', 'he', 'it', 'nl', 'no', 'pt', 'ru', 'sv', 'ud', 'zh'] as const;
/** Supported result ordering. */
export const SortOrders = ['publishedAt', 'relevancy', 'popularity'] as const;

/** NewsAPI settings; omitted or zero fields use manifest defaults. */
export interface Config {
  /** HTTPS API base URL or loopback test endpoint. */ baseUrl?: string;
  /** One HTTP request timeout in milliseconds. */ timeoutMs?: number;
  /** Maximum successful response size in bytes. */ maxResponseBytes?: number;
}

/** Private NewsAPI credential, resolved for every call. */
export interface Credentials { /** API key sent only in X-Api-Key. */ apiKey: SecretString }

/** One page request to NewsAPI's everything endpoint. */
export interface SearchArticlesInput {
  /** Keyword expression, at most 500 Unicode characters. */ query?: string;
  /** Article fields searched; empty searches all. */ searchIn?: readonly string[];
  /** At most 20 provider source IDs. */ sources?: readonly string[];
  /** Included domain names. */ domains?: readonly string[];
  /** Excluded domain names. */ excludeDomains?: readonly string[];
  /** Oldest date or ISO 8601 time. */ from?: string;
  /** Newest date or ISO 8601 time. */ to?: string;
  /** NewsAPI language code. */ language?: string;
  /** Provider result order. */ sortBy?: string;
  /** Results per page, 1 through 100; zero uses the provider default. */ pageSize?: number;
  /** One-based page; zero selects the first page. */ page?: number;
}

/** NewsAPI publisher identity. */
export interface ArticleSource { /** Source ID or null when absent. */ id: string | null; /** Publisher name. */ name: string }
/** One provider article; nullable fields preserve NewsAPI nulls. */
export interface Article {
  /** Publisher. */ source: ArticleSource;
  /** Byline. */ author: string | null;
  /** Untrusted headline; escape before HTML rendering. */ title: string;
  /** Untrusted summary; escape before HTML rendering. */ description: string | null;
  /** Publisher article URL. */ url: string;
  /** Lead image URL, if supplied. */ urlToImage: string | null;
  /** UTC publication time in RFC 3339 form. */ publishedAt: string;
  /** Untrusted body excerpt; escape before HTML rendering. */ content: string | null;
}
/** One page of matching articles. */
export interface ArticlePage {
  /** Count reported by NewsAPI across all pages. */ totalResults: number;
  /** Articles on this page, possibly empty. */ articles: Article[];
  /** One-based page number. */ page: number;
  /** Whether NewsAPI reports more matches after this page. */ hasMore: boolean;
}

const articleSourceSchema = z.object({ id: z.string().nullable(), name: z.string() });
const articleSchema = z.object({
  source: articleSourceSchema, author: z.string().nullable(), title: z.string(), description: z.string().nullable(),
  url: z.string(), urlToImage: z.string().nullable().optional(), publishedAt: z.iso.datetime({ offset: true }), content: z.string().nullable(),
});
const wireArticleSchema = z.object({
  source: z.object({ id: z.string().nullable().optional(), name: z.string().nullable().optional() }).nullable().optional(),
  author: z.string().nullable().optional(), title: z.string().nullable().optional(),
  description: z.string().nullable().optional(), url: z.string().nullable().optional(),
  urlToImage: z.string().nullable().optional(), publishedAt: z.iso.datetime({ offset: true }).nullable().optional(),
  content: z.string().nullable().optional(),
});
/** Runtime result validator for mock fixtures and provider responses. */
export const ArticlePageSchema: z.ZodType<ArticlePage> = z.object({
  totalResults: z.number().int().nonnegative(), articles: z.array(articleSchema.transform(article => ({ ...article, urlToImage: article.urlToImage ?? null }))),
  page: z.number().int().positive(), hasMore: z.boolean(),
});

/** Manifest-defined operation routes and Dex Execute defaults. */
export const SearchArticlesDefinition: QueryDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'searchArticles' },
  branches: [
    { id: SearchArticlesBranchSearched, description: 'NewsAPI returned one page of matching articles, possibly empty.' },
    { id: SearchArticlesBranchProviderRejected, description: 'NewsAPI conclusively rejected the search, such as an invalid or exhausted API key, a request the plan does not permit, or an unknown source.', optional: true },
    { id: SearchArticlesBranchInvalidResponse, description: 'NewsAPI returned malformed or oversized data.', optional: true },
    { id: SearchArticlesBranchDefect, description: 'Local input, connection configuration, or credentials are invalid.', optional: true },
  ],
  stepDefaults: { executeMethodTimeoutMs: 30_000, executeDurability: 'async', executeRetry: defaultConnectorStepExecuteRetryPolicy() },
};

/** Manifest fields outside the SDK QueryDefinition type. */
export const SearchArticlesMetadata = { kind: 'query', idempotency: 'none', authorization: 'required' } as const;

const emptyPage: ArticlePage = { totalResults: 0, articles: [], page: 0, hasMore: false };
const documentedCodes = new Set(['apiKeyExhausted', 'rateLimited', 'unexpectedError', 'maximumResultsReached', 'apiKeyDisabled', 'apiKeyInvalid', 'apiKeyMissing', 'parameterInvalid', 'parametersMissing', 'sourcesTooMany', 'sourceDoesNotExist']);
const authenticationCodes = new Set(['apiKeyDisabled', 'apiKeyInvalid', 'apiKeyMissing']);
const parameterCodes = new Set(['parameterInvalid', 'parametersMissing', 'sourcesTooMany', 'sourceDoesNotExist']);

function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: ConnectorID, operation: 'searchArticles', message }; }
function receipt(call: Call) { return { callId: call.id, provider: ConnectorID, observedAt: new Date().toISOString() }; }
function failedBranch(call: Call, branch: string, kind: Failure['kind'], message: string): QueryAttempt<ArticlePage> {
  return newQueryBranch(branch, emptyPage, receipt(call), failure(kind, message));
}

/** One read-only NewsAPI operation, bound to immutable settings and a live credential source. */
export class SearchArticlesOperation implements Query<SearchArticlesInput, ArticlePage> {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;

  /** Validate local configuration without making a provider request. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>) {
    if (!credentials) throw new TypeError('NewsAPI credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://newsapi.org/v2');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('configuration timeout cannot be negative');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('configuration maxResponseBytes cannot be negative');
  }

  /** Return stable operation metadata for generated Step factories. */
  definition(): QueryDefinition { return SearchArticlesDefinition; }

  /** Validate, send once, and classify one provider attempt. */
  async invoke(call: Call, input: SearchArticlesInput): Promise<QueryAttempt<ArticlePage>> {
    let query: URLSearchParams;
    try { query = encodeSearchQuery(input); }
    catch (error) { return failedBranch(call, SearchArticlesBranchDefect, 'VALIDATION', error instanceof Error ? error.message : 'NewsAPI search input is invalid'); }
    let resolved: Credentials;
    try { resolved = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return failedBranch(call, SearchArticlesBranchDefect, 'AUTHENTICATION', 'connection credentials are unavailable'); }
    if (!(resolved.apiKey instanceof SecretString) || !isHeaderSafeCredential(resolved.apiKey.reveal())) {
      return failedBranch(call, SearchArticlesBranchDefect, 'AUTHENTICATION', 'connection credentials are invalid');
    }
    try {
      const response = await sendProviderRequest({
        url: `${this.baseUrl}/everything?${query.toString()}`, method: 'GET',
        headers: { 'X-Api-Key': resolved.apiKey.reveal(), Accept: 'application/json', 'User-Agent': 'superdurable-newsapi/0.21' },
        maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), timeoutMs: this.timeoutMs, addressPolicy: 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
      if (response.status !== 200) return classifyErrorResponse(call, response.status, response.body, response.headers['retry-after']);
      if (response.body.length > this.maxResponseBytes) return failedBranch(call, SearchArticlesBranchInvalidResponse, 'RESPONSE_TOO_LARGE', 'NewsAPI response exceeded the configured limit');
      return decodeArticlePage(call, response.body, input);
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return classifyErrorResponse(call, error.status, new Uint8Array(), error.headers?.['retry-after']);
        return failedBranch(call, SearchArticlesBranchInvalidResponse, 'RESPONSE_TOO_LARGE', 'NewsAPI response exceeded the configured limit');
      }
      if (error instanceof AddressRefusal) return failedBranch(call, SearchArticlesBranchDefect, 'LOCAL_DEFECT', 'NewsAPI request could not be built');
      if (error instanceof ProviderTransportError) return newQueryRetry(failure('AVAILABILITY', 'NewsAPI request failed'));
      return newQueryRetry(failure('AVAILABILITY', 'NewsAPI request failed'));
    }
  }
}

/** NewsAPI client owning validated settings and a live credential source. */
export class Client {
  /** Read-only search operation used by generated Step factories. */
  readonly searchArticles: SearchArticlesOperation;

  /** Construct without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>) {
    this.searchArticles = new SearchArticlesOperation(config, credentials);
  }
}

/** Operations accepted by a connection, including scripted implementations. */
export interface Operations {
  /** Search one NewsAPI article page. */ searchArticles: Query<SearchArticlesInput, ArticlePage>;
}

/** Bind a real client or scripted operations to a named logical connection. */
export class Connection {
  /** Search operation invoked by the factory. */ readonly searchArticles: Query<SearchArticlesInput, ArticlePage>;

  /** Construct a connection with its client or scripted operation set. */
  constructor(readonly reference: ConnectionRef, readonly operations: Operations, readonly client?: Client) {
    if (reference.provider !== ConnectorID || !reference.name.trim() || !operations?.searchArticles) throw new TypeError('NewsAPI connection is invalid');
    this.searchArticles = operations.searchArticles;
  }
}

/** Bind a validated NewsAPI client to a named logical connection. */
export function newConnection(client: Client, reference: ConnectionRef): Connection {
  if (!client) throw new TypeError('NewsAPI client is required');
  return new Connection(reference, { searchArticles: client.searchArticles }, client);
}

/** Bind scripted operations to a named logical connection for tests. */
export function newConnectionWithOperations(operations: Operations, reference: ConnectionRef): Connection {
  return new Connection(reference, operations);
}

/** Open a declared Dex Web connection; private credentials are resolved at each call. */
export function newProjectConnection(project: LoadedProject, connectionName: string): Connection {
  if (!project || !connectionName.trim()) throw new TypeError('NewsAPI project connection is required');
  const configured = project.configuration.connections.find(entry => entry.connectorId === ConnectorID && entry.connectionName === connectionName);
  if (!configured) throw new TypeError('NewsAPI project connection is not configured');
  const fields = z.strictObject({ baseUrl: z.string().optional(), timeout: z.union([z.string(), z.number().int()]).optional(), maxResponseBytes: z.number().int().optional() }).parse(configured.configuration);
  const timeoutMs = typeof fields.timeout === 'number' ? Math.ceil(fields.timeout / 1_000_000) : fields.timeout ? parseDurationMilliseconds(fields.timeout) : undefined;
  const config: Config = { baseUrl: fields.baseUrl, timeoutMs, maxResponseBytes: fields.maxResponseBytes };
  const credentials: CredentialProvider<Credentials> = {
    async resolve(_call, signal) {
      const privateFields = await project.resolveCredential(ConnectorID, connectionName, signal);
      if (typeof privateFields.api_key !== 'string') throw new TypeError('NewsAPI API key is unavailable');
      return { apiKey: new SecretString(privateFields.api_key) };
    },
  };
  return newConnection(new Client(config, credentials), { provider: ConnectorID, name: connectionName });
}

/** Operation-specific factory settings with a static Dex Web connection name. */
export interface SearchArticlesStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, SearchArticlesInput, ArticlePage>, 'operation' | 'connection' | 'branches'> {
  /** Typed real or mock NewsAPI connection. */ connection: Connection;
  /** Static name also declared by the consuming Flow. */ connectionName: string;
  /** Required successful route. */ searched: StepTarget<QueryResult<ArticlePage>>;
  /** Optional conclusive provider rejection route; omission fails the Flow. */ providerRejected?: StepTarget<QueryResult<ArticlePage>>;
  /** Optional invalid provider response route; omission fails the Flow. */ invalidResponse?: StepTarget<QueryResult<ArticlePage>>;
  /** Optional local input or credential defect route; omission fails the Flow. */ defect?: StepTarget<QueryResult<ArticlePage>>;
}

/** Create the manifest-backed searchArticles Query Step for one Flow. */
export function newSearchArticlesStep<StepInput>(configuration: SearchArticlesStepConfig<StepInput>): QueryStep<StepInput, SearchArticlesInput, ArticlePage> {
  const { connection, connectionName, searched, providerRejected, invalidResponse, defect, ...stepConfiguration } = configuration;
  if (connection.reference.name !== connectionName) throw new TypeError('NewsAPI Step connection name differs from the bound connection');
  return newQueryStep({
    ...stepConfiguration, operation: connection.searchArticles, connection: connection.reference,
    branches: [
      { branch: SearchArticlesBranchSearched, target: searched },
      ...(providerRejected ? [{ branch: SearchArticlesBranchProviderRejected, target: providerRejected }] : []),
      ...(invalidResponse ? [{ branch: SearchArticlesBranchInvalidResponse, target: invalidResponse }] : []),
      ...(defect ? [{ branch: SearchArticlesBranchDefect, target: defect }] : []),
    ],
  });
}

function parseDurationMilliseconds(value: string): number {
  let remaining = value;
  let totalMilliseconds = 0;
  const unitMilliseconds: Record<string, number> = { ns: 0.000001, us: 0.001, 'µs': 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  while (remaining) {
    const component = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/.exec(remaining);
    if (!component) throw new TypeError('NewsAPI timeout must be a positive Go duration');
    totalMilliseconds += Number(component[1]) * (unitMilliseconds[component[2] ?? ''] ?? NaN);
    remaining = remaining.slice(component[0].length);
  }
  if (!Number.isSafeInteger(totalMilliseconds) || totalMilliseconds <= 0) throw new TypeError('NewsAPI timeout must resolve to positive whole milliseconds');
  return totalMilliseconds;
}

function encodeSearchQuery(input: SearchArticlesInput): URLSearchParams {
  const query = new URLSearchParams();
  if ([...input.query ?? ''].length > 500) throw new TypeError('query must be at most 500 characters');
  if (!(input.query ?? '').trim() && !input.sources?.length && !input.domains?.length) throw new TypeError('query, sources, or domains must be set');
  if ((input.query ?? '').trim()) query.set('q', input.query ?? '');
  if (input.searchIn?.some(value => !(SearchInFields as readonly string[]).includes(value))) throw new TypeError('searchIn accepts only title, description, and content');
  if (input.searchIn?.length) query.set('searchIn', input.searchIn.join(','));
  if ((input.sources?.length ?? 0) > 20) throw new TypeError('sources accepts at most 20 source IDs');
  for (const [name, values, pattern] of [
    ['sources', input.sources, /^[a-z0-9][a-z0-9-]{0,99}$/],
    ['domains', input.domains, /^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$/],
    ['excludeDomains', input.excludeDomains, /^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$/],
  ] as const) {
    if (values?.some(value => !pattern.test(value))) throw new TypeError(`${name} contains an invalid entry`);
    if (values?.length) query.set(name, values.join(','));
  }
  const from = setTime(query, 'from', input.from);
  const to = setTime(query, 'to', input.to);
  if (from !== undefined && to !== undefined && from > to) throw new TypeError('from must not be after to');
  if (input.language) {
    if (!(Languages as readonly string[]).includes(input.language)) throw new TypeError(`language must be one of ${Languages.join(', ')}`);
    query.set('language', input.language);
  }
  if (input.sortBy) {
    if (!(SortOrders as readonly string[]).includes(input.sortBy)) throw new TypeError('sortBy must be publishedAt, relevancy, or popularity');
    query.set('sortBy', input.sortBy);
  }
  if (input.pageSize !== undefined && (!Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 100)) throw new TypeError('pageSize must be from 1 through 100, or zero for NewsAPI\'s default');
  if (input.pageSize) query.set('pageSize', String(input.pageSize));
  if (input.page !== undefined && (!Number.isSafeInteger(input.page) || input.page < 0)) throw new TypeError('page must be positive, or zero for the first page');
  if (input.page) query.set('page', String(input.page));
  return query;
}

function setTime(query: URLSearchParams, name: string, value: string | undefined): number | undefined {
  if (!value) return undefined;
  if (!/^\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)?$/.test(value)) throw new TypeError(`${name} must be a date such as 2026-01-05 or an ISO 8601 time`);
  const year = Number(value.slice(0, 4));
  const month = Number(value.slice(5, 7));
  const day = Number(value.slice(8, 10));
  const dateProbe = new Date(0);
  dateProbe.setUTCFullYear(year, month - 1, day);
  if (dateProbe.toISOString().slice(0, 10) !== value.slice(0, 10)) throw new TypeError(`${name} must be a date such as 2026-01-05 or an ISO 8601 time`);
  const timestamp = Date.parse(value.endsWith('Z') || /[+-]\d{2}:\d{2}$/.test(value) ? value : `${value}${value.length === 10 ? 'T00:00:00Z' : 'Z'}`);
  if (Number.isNaN(timestamp)) throw new TypeError(`${name} must be a date such as 2026-01-05 or an ISO 8601 time`);
  query.set(name, value);
  return timestamp;
}

function classifyErrorResponse(call: Call, status: number, body: Uint8Array, retryHeader: string | string[] | undefined): QueryAttempt<ArticlePage> {
  const code = readErrorTokens(body.subarray(0, MaxErrorBodyBytes), ['/code'])[0] ?? '';
  const describedCode = documentedCodes.has(code) ? code : 'no documented error code';
  const retryAfterMs = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '');
  if (code === 'apiKeyExhausted') return failedBranch(call, SearchArticlesBranchProviderRejected, 'QUOTA_EXHAUSTED', `NewsAPI reports the API key's request allowance is exhausted (${describedCode})`);
  if (status === 429) return newQueryRetry(failure('RATE_LIMIT', `NewsAPI rate limited the request (${describedCode})`), retryAfterMs);
  if (status === 408 || status >= 500) return newQueryRetry(failure('AVAILABILITY', `NewsAPI is temporarily unavailable (${describedCode})`), retryAfterMs);
  if (status === 401 || authenticationCodes.has(code)) return failedBranch(call, SearchArticlesBranchProviderRejected, 'AUTHENTICATION', `NewsAPI rejected the API key (${describedCode})`);
  if (status === 426) return failedBranch(call, SearchArticlesBranchProviderRejected, 'AUTHORIZATION', `NewsAPI's plan does not permit this request (${describedCode})`);
  if (parameterCodes.has(code)) return failedBranch(call, SearchArticlesBranchProviderRejected, 'VALIDATION', `NewsAPI rejected the search parameters (${describedCode})`);
  return failedBranch(call, SearchArticlesBranchProviderRejected, 'PROVIDER_REJECTION', `NewsAPI rejected the request with HTTP ${status} (${describedCode})`);
}

function decodeArticlePage(call: Call, body: Uint8Array, input: SearchArticlesInput): QueryAttempt<ArticlePage> {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { return failedBranch(call, SearchArticlesBranchInvalidResponse, 'PROTOCOL', 'NewsAPI returned malformed JSON'); }
  const parsed = z.object({ status: z.literal('ok'), totalResults: z.number().int().nonnegative(), articles: z.array(wireArticleSchema) }).safeParse(document);
  if (!parsed.success) return failedBranch(call, SearchArticlesBranchInvalidResponse, 'PROTOCOL', 'NewsAPI returned an incomplete article page');
  const page = input.page || 1;
  const pageSize = input.pageSize || 100;
  if (parsed.data.articles.length > pageSize) return failedBranch(call, SearchArticlesBranchInvalidResponse, 'PROTOCOL', 'NewsAPI returned more articles than the page size');
  const articles: Article[] = parsed.data.articles.map(article => ({
    source: { id: article.source?.id ?? null, name: article.source?.name ?? '' },
    author: article.author ?? null, title: article.title ?? '', description: article.description ?? null,
    url: article.url ?? '', urlToImage: article.urlToImage ?? null,
    publishedAt: article.publishedAt ? new Date(article.publishedAt).toISOString().replace('.000Z', 'Z') : '0001-01-01T00:00:00Z',
    content: article.content ?? null,
  }));
  return newQueryBranch(SearchArticlesBranchSearched, { totalResults: parsed.data.totalResults, articles, page, hasMore: page * pageSize < parsed.data.totalResults }, receipt(call));
}
