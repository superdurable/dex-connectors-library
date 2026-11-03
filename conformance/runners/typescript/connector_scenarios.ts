// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { isDeepStrictEqual } from 'node:util';

type RecordValue = Record<string, unknown>;
type RequestValues = Record<string, string | string[]>;
export interface Exchange {
  method: string; path: string; keyMode: 'absent' | 'equals-call-id' | 'stable-nonempty' | 'derived';
  status?: number; headers?: Record<string, string>; body?: string; query?: RequestValues;
  form?: RequestValues; requestBody?: unknown; requestHeaders?: RequestValues;
  transportFailure?: 'before-send' | 'after-send'; derivedKey?: string;
}
export interface Expected {
  branch: string; failureKind: string; retry: boolean; retryAfterMs: number;
  uncertain: boolean; requestCount: number; output?: unknown;
}
export interface Scenario {
  name: string; operation: string; callId: string; idempotencyKey?: string; input: RecordValue;
  exchanges: Exchange[]; expected: Expected; attempts?: number;
  intermediate?: Array<{ failureKind: string; retryAfterMs: number; exchangeCount: number }>;
  priorHeartbeat?: unknown;
  clock?: string;
}

export function scenarioClock(scenario: Scenario): () => Date {
  if (scenario.clock === undefined) return () => new Date();
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(scenario.clock)) {
    throw new Error(`${scenario.name}: clock must be an RFC3339 timestamp`);
  }
  const milliseconds = Date.parse(scenario.clock);
  if (!Number.isFinite(milliseconds)) throw new Error(`${scenario.name}: invalid clock timestamp`);
  return () => new Date(milliseconds);
}

export class HeartbeatContext {
  attempt: number;
  marker: unknown;
  readonly runId = 'run';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  readonly flowId = 'conformance-flow';
  constructor(priorHeartbeat: unknown) {
    this.marker = priorHeartbeat;
    this.attempt = priorHeartbeat === undefined ? 1 : Number((priorHeartbeat as { attempt: number }).attempt) + 1;
  }
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

function record(value: unknown, required: string[], optional: string[], location: string): RecordValue {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${location}: expected object`);
  const fields = value as RecordValue;
  if (required.some(key => !(key in fields)) || Object.keys(fields).some(key => !required.includes(key) && !optional.includes(key))) {
    throw new Error(`${location}: missing or unknown fields`);
  }
  return fields;
}

export function loadScenarios(url: URL): Scenario[] {
  const fixture = process.env.CONNECTOR_CONFORMANCE_FIXTURE ?? url;
  const document = record(JSON.parse(readFileSync(fixture, 'utf8')), ['schemaVersion', 'connector', 'scenarios'], [], url.pathname);
  if (document.schemaVersion !== 1 || typeof document.connector !== 'string' || !document.connector ||
      !Array.isArray(document.scenarios) || document.scenarios.length === 0) throw new Error('invalid scenario document');
  for (const item of document.scenarios) {
    const scenario = record(item, ['name', 'operation', 'callId', 'input', 'exchanges', 'expected'], ['idempotencyKey', 'attempts', 'intermediate', 'priorHeartbeat', 'clock'], url.pathname);
    if (typeof scenario.name !== 'string' || !scenario.name || typeof scenario.operation !== 'string' || !scenario.operation ||
        typeof scenario.callId !== 'string' || !scenario.callId || !Array.isArray(scenario.exchanges)) throw new Error('invalid scenario identity or exchanges');
    if ('idempotencyKey' in scenario && typeof scenario.idempotencyKey !== 'string') throw new Error('invalid optional scenario identity');
    if ('clock' in scenario) scenarioClock(scenario as unknown as Scenario);
    if (typeof scenario.input !== 'object' || scenario.input === null || Array.isArray(scenario.input)) {
      throw new Error(`${scenario.name}: input must be an object`);
    }
    for (const item of scenario.exchanges) {
      const exchange = record(item, ['method', 'path', 'keyMode'], ['status', 'headers', 'body', 'query', 'form', 'requestBody', 'requestHeaders', 'transportFailure', 'derivedKey'], String(scenario.name));
      if (('status' in exchange) === ('transportFailure' in exchange)) throw new Error(`${scenario.name}: expected one response or transport failure`);
      if (typeof exchange.method !== 'string' || !exchange.method || typeof exchange.path !== 'string' || !exchange.path) throw new Error('invalid request method or path');
      if ('status' in exchange && (!('headers' in exchange) || !('body' in exchange))) throw new Error(`${scenario.name}: response requires headers and body`);
      if ('status' in exchange && (typeof exchange.status !== 'number' || !Number.isInteger(exchange.status) ||
          typeof exchange.headers !== 'object' || exchange.headers === null || Array.isArray(exchange.headers) ||
          typeof exchange.body !== 'string')) throw new Error('invalid response');
      if ('transportFailure' in exchange && ('headers' in exchange || 'body' in exchange)) throw new Error(`${scenario.name}: transport failure cannot return a response`);
      if ('transportFailure' in exchange && !['before-send', 'after-send'].includes(String(exchange.transportFailure))) throw new Error('invalid transport failure');
      if (!['absent', 'equals-call-id', 'stable-nonempty', 'derived'].includes(String(exchange.keyMode))) throw new Error('invalid key mode');
      if (exchange.keyMode === 'derived' && !exchange.derivedKey) throw new Error('derived key is missing');
      for (const field of ['query', 'form', 'requestHeaders']) {
        if (field in exchange && (typeof exchange[field] !== 'object' || exchange[field] === null || Array.isArray(exchange[field]) ||
            Object.values(exchange[field] as RecordValue).some(value => typeof value !== 'string' &&
              !(Array.isArray(value) && value.length > 0 && value.every(item => typeof item === 'string'))))) throw new Error(`invalid ${field}`);
      }
      const body = exchange.requestBody;
      if (typeof body === 'object' && body !== null && !Array.isArray(body) && '$rawMimeContains' in body) {
        const fragments = (body as RecordValue).$rawMimeContains;
        if (Object.keys(body).some(key => !['$rawMimeContains', 'threadId'].includes(key)) ||
            ('threadId' in body && (typeof body.threadId !== 'string' || !body.threadId)) ||
            !Array.isArray(fragments) || !fragments.length ||
            fragments.some(fragment => typeof fragment !== 'string' || !fragment)) throw new Error('invalid raw MIME assertions');
      }
    }
    const expected = record(scenario.expected, ['branch', 'failureKind', 'retry', 'retryAfterMs', 'uncertain', 'requestCount'], ['output'], String(scenario.name));
    if (typeof expected.branch !== 'string' || typeof expected.failureKind !== 'string' || typeof expected.retry !== 'boolean' ||
        typeof expected.uncertain !== 'boolean' || !Number.isInteger(expected.retryAfterMs) ||
        !Number.isInteger(expected.requestCount)) throw new Error('invalid expected decision');
    const intermediate = scenario.intermediate ?? [];
    if (!Array.isArray(intermediate) || (scenario.attempts ?? 1) !== intermediate.length + 1) throw new Error(`${scenario.name}: inconsistent attempts`);
    for (const value of intermediate) record(value, ['failureKind', 'retryAfterMs', 'exchangeCount'], [], String(scenario.name));
    if ('priorHeartbeat' in scenario) {
      const prior = record(scenario.priorHeartbeat, ['callId', 'attempt', 'state'], ['providerId'], String(scenario.name));
      if (prior.callId !== scenario.callId) throw new Error('prior heartbeat Call ID differs');
      if (!Number.isInteger(prior.attempt) || Number(prior.attempt) < 1 ||
          !['notSent', 'claimed', 'canResend'].includes(String(prior.state)) ||
          ('providerId' in prior && typeof prior.providerId !== 'string')) throw new Error('invalid prior heartbeat');
    }
  }
  return document.scenarios as Scenario[];
}

export interface RecordedRequest { method: string; url: string; headers?: Record<string, string | string[] | undefined>; body?: string | Uint8Array | undefined }
export function scenarioCall(scenario: Scenario, connection: { provider: string; name: string }, connectorId: string,
  context: unknown, extra: Record<string, unknown> = {}) {
  return { ...extra, id: scenario.callId,
    connection, operation: { connectorId, operationId: scenario.operation }, context };
}

export async function invokeProductionMutation<Input, Output, InvocationCall extends { idempotencyKey?: string }>(
  operation: { definition(): unknown; idempotencyKey(callId: string, input: Input): string; invoke(call: InvocationCall, input: Input): Output | Promise<Output> },
  expectedKey: string, runProduction: (recording: unknown) => Promise<unknown>,
  isRetryError: (error: unknown) => boolean,
): Promise<Output> {
  let observedKey = '';
  let observedAttempt: Output | undefined;
  const recording = {
    definition: () => operation.definition(),
    idempotencyKey: (callId: string, value: Input) => operation.idempotencyKey(callId, value),
    invoke: async (invocationCall: InvocationCall, value: Input) => {
      observedKey = invocationCall.idempotencyKey ?? '';
      observedAttempt = await operation.invoke(invocationCall, value);
      return observedAttempt;
    },
  };
  try { await runProduction(recording); }
  catch (error) { if (!isRetryError(error)) throw error; }
  if (observedKey !== expectedKey) throw new Error(`production idempotency key ${observedKey} differs from ${expectedKey}`);
  if (!observedAttempt) throw new Error('operation did not produce an attempt');
  return observedAttempt;
}

export async function answerRecordedRequest(recorder: ExchangeRecorder, request: RecordedRequest,
  transportError: (beforeSend: boolean) => Error): Promise<{ status: number; headers: Record<string, string>; body: Uint8Array }> {
  const exchange = recorder.record(request);
  if (exchange.transportFailure) throw transportError(exchange.transportFailure === 'before-send');
  return { status: exchange.status ?? 599,
    headers: Object.fromEntries(Object.entries(exchange.headers ?? {}).map(([key, value]) => [key.toLowerCase(), value])),
    body: Buffer.from(exchange.body ?? '') };
}

export async function withScenarioHTTPServer<Value>(recorder: ExchangeRecorder,
  use: (baseUrl: string) => Promise<Value>): Promise<Value> {
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const exchange = recorder.record({ method: request.method ?? '', url: request.url ?? '/', headers: request.headers, body: Buffer.concat(chunks) });
    response.writeHead(exchange.status ?? 599, exchange.headers ?? {});
    response.end(exchange.body ?? '');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (typeof address === 'string' || address === null) throw new Error('missing server address');
  try { return await use(`http://127.0.0.1:${address.port}`); }
  finally { await new Promise<void>(resolve => server.close(() => resolve())); }
}

export class ExchangeRecorder {
  readonly mismatches: string[] = [];
  requestCount = 0;
  providerRequestCount = 0;
  private stableKey = '';
  constructor(readonly scenario: Scenario, readonly pathPrefix = '') {}

  record(request: RecordedRequest): Exchange {
    const index = this.requestCount++;
    const exchange = this.scenario.exchanges[index];
    if (!exchange) {
      this.mismatches.push(`request ${index}: unexpected provider request`);
      return { method: request.method, path: '', keyMode: 'absent', status: 599, headers: {}, body: '' };
    }
    if (exchange.transportFailure !== 'before-send') this.providerRequestCount++;
    const url = new URL(request.url, 'http://localhost');
    const bodyText = typeof request.body === 'string' ? request.body : request.body ? Buffer.from(request.body).toString() : '';
    this.equal(index, 'method', request.method, exchange.method);
    this.equal(index, 'path', url.pathname, this.pathPrefix + exchange.path);
    for (const [key, value] of Object.entries(exchange.query ?? {})) this.equal(index, `query ${key}`, url.searchParams.getAll(key), Array.isArray(value) ? value : [value]);
    for (const key of new Set(url.searchParams.keys())) if (!(key in (exchange.query ?? {}))) this.mismatches.push(`request ${index}: unasserted query ${key}=${JSON.stringify(url.searchParams.getAll(key))}`);
    const contentType = Object.entries(request.headers ?? {}).find(([name]) => name.toLowerCase() === 'content-type')?.[1];
    const form = typeof contentType === 'string' && contentType.startsWith('application/x-www-form-urlencoded') ? new URLSearchParams(bodyText) : new URLSearchParams();
    for (const [key, value] of Object.entries(exchange.form ?? {})) this.equal(index, `form ${key}`, form.getAll(key), Array.isArray(value) ? value : [value]);
    for (const key of new Set(form.keys())) if (!(key in (exchange.form ?? {}))) this.mismatches.push(`request ${index}: unasserted form ${key}=${JSON.stringify(form.getAll(key))}`);
    if (exchange.requestBody !== undefined) {
      const expected = exchange.requestBody;
      if (typeof expected === 'object' && expected !== null && !Array.isArray(expected) && '$rawMimeContains' in expected) {
        let mime = '';
        try {
          const payload = JSON.parse(bodyText) as RecordValue;
          const expectedKeys = 'threadId' in expected ? ['raw', 'threadId'] : ['raw'];
          if (!isDeepStrictEqual(Object.keys(payload).sort(), expectedKeys.sort())) this.mismatches.push(`request ${index}: invalid MIME request body`);
          if ('threadId' in expected) this.equal(index, 'threadId', payload.threadId, expected.threadId);
          mime = Buffer.from(payload.raw as string, 'base64url').toString();
        } catch { /* A missing or invalid raw field fails each required fragment. */ }
        for (const fragment of (expected as { $rawMimeContains: string[] }).$rawMimeContains) {
          if (!mime.includes(fragment)) this.mismatches.push(`request ${index}: MIME body lacks ${JSON.stringify(fragment)}`);
        }
      } else if (typeof expected === 'object' && expected !== null && !Array.isArray(expected) && '$rawMime' in expected) {
        try {
          const payload = JSON.parse(bodyText) as RecordValue;
          const expectedKeys = 'threadId' in expected ? ['raw', 'threadId'] : ['raw'];
          if (!isDeepStrictEqual(Object.keys(payload).sort(), expectedKeys.sort())) this.mismatches.push(`request ${index}: invalid MIME request body`);
          if ('threadId' in expected) this.equal(index, 'threadId', payload.threadId, expected.threadId);
          this.equal(index, 'MIME body', Buffer.from(payload.raw as string, 'base64url').toString(), expected.$rawMime);
        } catch { this.mismatches.push(`request ${index}: invalid raw MIME encoding`); }
      } else {
        let body: unknown = bodyText;
        if (typeof expected === 'object' && expected !== null) {
          try { body = JSON.parse(bodyText); } catch { /* Keep the raw body for mismatch reporting. */ }
        }
        this.equal(index, 'body', body, expected);
      }
    } else if (!exchange.form && bodyText) {
      this.mismatches.push(`request ${index}: unasserted body`);
    }
    const headers = Object.fromEntries(Object.entries(request.headers ?? {}).map(([name, value]) => [name.toLowerCase(), value]));
    const headerValues = (name: string): string[] => {
      const value = headers[name.toLowerCase()];
      return value === undefined ? [] : Array.isArray(value) ? value : [value];
    };
    for (const [name, value] of Object.entries(exchange.requestHeaders ?? {})) this.equal(index, `header ${name}`, headerValues(name), Array.isArray(value) ? value : [value]);
    const assertedHeaders = new Set(Object.keys(exchange.requestHeaders ?? {}).map(name => name.toLowerCase()));
    for (const [name, value] of Object.entries(headers)) {
      if (['host', 'accept-encoding', 'connection', 'content-length', 'idempotency-key'].includes(name)) continue;
      if (name === 'accept' && value === '*/*' || name === 'user-agent' && typeof value === 'string' && value.startsWith('node/')) continue;
      if (!assertedHeaders.has(name)) this.mismatches.push(`request ${index}: unasserted header ${name}=${JSON.stringify(value)}`);
    }
    const keyValues = headerValues('Idempotency-Key');
    const key = keyValues.length === 1 ? keyValues[0] ?? '' : '';
    switch (exchange.keyMode) {
      case 'absent': this.equal(index, 'Idempotency-Key', keyValues, []); break;
      case 'equals-call-id': this.equal(index, 'Idempotency-Key', keyValues, [this.scenario.callId]); break;
      case 'stable-nonempty':
        if (keyValues.length !== 1 || !key) this.mismatches.push(`request ${index}: Idempotency-Key needs one nonempty value`);
        if (this.stableKey && key !== this.stableKey) this.mismatches.push(`request ${index}: Idempotency-Key changed`);
        this.stableKey = key; break;
      case 'derived': this.equal(index, 'Idempotency-Key', keyValues, [exchange.derivedKey ?? '']); break;
    }
    return exchange;
  }

  finish(): void {
    if (this.requestCount !== this.scenario.exchanges.length) throw new Error('unexpected transport attempt count');
    if (this.providerRequestCount !== this.scenario.expected.requestCount) throw new Error(`provider request count ${this.providerRequestCount} differs from expected ${this.scenario.expected.requestCount}`);
    if (this.mismatches.length) throw new Error(this.mismatches.join('\n'));
  }

  private equal(index: number, field: string, actual: unknown, expected: unknown): void {
    if (!isDeepStrictEqual(actual, expected)) this.mismatches.push(`request ${index}: ${field}: ${JSON.stringify(actual)} != ${JSON.stringify(expected)}`);
  }
}

export interface ScenarioOutputCodec { encode(value: unknown): { kind: string; data: unknown } }
function assertAttempt(attempt: { kind: string; branch?: string; failure?: { kind: string }; retryAfterMs?: number; value?: unknown }, expected: Expected, outputCodec: ScenarioOutputCodec): void {
  const kind = expected.retry ? 'retry' : expected.uncertain ? 'uncertain' : 'branch';
  if (attempt.kind !== kind || (attempt.branch ?? (attempt.kind === 'uncertain' ? 'uncertain' : '')) !== expected.branch ||
      (attempt.failure?.kind ?? '') !== expected.failureKind || (attempt.retryAfterMs ?? 0) !== expected.retryAfterMs) {
    throw new Error(`attempt ${JSON.stringify(attempt)} differs from ${JSON.stringify(expected)}`);
  }
  if ('output' in expected) {
    const encoded = outputCodec.encode(attempt.value);
    if (encoded.kind !== 'json' || typeof encoded.data !== 'string') throw new Error('SDK output codec did not encode JSON');
    const output = JSON.parse(encoded.data);
    if (!isDeepStrictEqual(output, expected.output)) {
      throw new Error(`attempt output ${JSON.stringify(output)} differs from ${JSON.stringify(expected.output)}`);
    }
  }
}

export async function replay(
  scenario: Scenario,
  invoke: (scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) => Promise<{ kind: string; branch?: string; failure?: { kind: string }; retryAfterMs?: number; value?: unknown }>,
  pathPrefix = '',
): Promise<void> {
  const sdkPackageDirectory = join(process.cwd(), 'node_modules', '@superdurable', 'dex');
  const sdkPackage = JSON.parse(readFileSync(join(sdkPackageDirectory, 'package.json'), 'utf8')) as {
    exports?: { '.': { import?: string } };
  };
  const sdkEntry = sdkPackage.exports?.['.']?.import;
  if (!sdkEntry) throw new Error('Dex SDK has no importable codec entrypoint');
  const dexSDK = await import(pathToFileURL(join(sdkPackageDirectory, sdkEntry)).href) as {
    jsonCodec: () => ScenarioOutputCodec;
  };
  const outputCodec = dexSDK.jsonCodec();
  const recorder = new ExchangeRecorder(scenario, pathPrefix);
  const context = new HeartbeatContext(scenario.priorHeartbeat);
  for (const intermediate of scenario.intermediate ?? []) {
    const attempt = await invoke(scenario, recorder, context);
    assertAttempt(attempt, { retry: true, uncertain: false, branch: '', requestCount: 0, ...intermediate }, outputCodec);
    if (recorder.requestCount !== intermediate.exchangeCount) throw new Error('intermediate exchange count differs');
    context.attempt++;
  }
  const attempt = await invoke(scenario, recorder, context);
  recorder.finish();
  assertAttempt(attempt, scenario.expected, outputCodec);
}
