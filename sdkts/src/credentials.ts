// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ConnectionRefSchema, OperationRefSchema, deriveCallID, type CallID, type ConnectionRef, type OperationRef } from './types.js';
import type { AsyncContext, BufferedTextStreamOptions, Stream } from '@superdurable/dex';

/** Provider-safe progress before Dex identity and sequence are added. */
export interface Progress {
  /** Provider-specific stable phase. */ phase: string;
  /** Optional safe progress text. */ message?: string;
  /** Optional inclusive fraction from zero to one. */ percent?: number;
  /** Optional provider-owned monotonic sequence. */ providerSequence?: number;
}

/** Structured best-effort Dex Stream message. */
export interface ProgressUpdate extends Progress {
  /** Stable connector call ID. */ callId: CallID;
  /** One-based Dex Execute attempt. */ attempt: number;
  /** Connector-assigned monotonic sequence within this attempt. */ sequence: number;
}

/** Optional application-owned Streams for one connector invocation. */
export interface CallProgressOptions {
  /** Registered structured progress Stream. */ progressStream?: Stream<ProgressUpdate>;
  /** Registered string Stream for ordered buffered text. */ textStream?: Stream<string>;
  /** Flush interval and soft UTF-8 batch limit for text. */ textOptions?: BufferedTextStreamOptions;
}

/** Provider-safe call metadata with its live, non-serializable Dex Step context. */
export interface Call {
  /** Active Dex Execute context; Step factories attach it as a non-enumerable field. */ context: AsyncContext;
  /** Stable identity across retries. */ id: CallID;
  /** Provider deduplication key, when available. */ idempotencyKey?: string;
  /** Logical named connection. */ connection: ConnectionRef;
  /** Manifest operation identity. */ operation: OperationRef;
  /** Append structured progress when a Stream is configured. */ reportProgress(progress: Progress): void;
  /** Append text in order when a buffered Stream is configured. */ writeText(value: string): void;
  /** Whether structured progress output is configured. */ hasProgressStream(): boolean;
  /** Whether buffered text output is configured. */ hasTextStream(): boolean;
}

/** Bind a live Dex Execute context to a stable, provider-safe call identity. */
export function createCall(context: AsyncContext, connection: ConnectionRef, operation: OperationRef, options: CallProgressOptions = {}): Call {
  if (!context?.flowId) throw new TypeError('Dex Flow ID is required');
  if (!context.stepExecutionId) throw new TypeError('connector operations require a Dex Step execution; RPC invocation is not allowed');
  ConnectionRefSchema.parse(connection);
  OperationRefSchema.parse(operation);
  const call = { context, id: deriveCallID(context.flowId, context.stepExecutionId, operation), connection, operation } as Call;
  const textWriter = options.textStream?.bufferedText(context, options.textOptions);
  let sequence = 0;
  Object.defineProperty(call, 'context', { value: context, enumerable: false });
  Object.defineProperties(call, {
    reportProgress: { value: (progress: Progress) => {
      if (!options.progressStream) return;
      if (progress.percent !== undefined && (!Number.isFinite(progress.percent) || progress.percent < 0 || progress.percent > 1)) throw new RangeError('connector progress percent must be between 0 and 1');
      options.progressStream.write(context, { ...progress, callId: call.id, attempt: context.attempt, sequence: ++sequence });
    }, enumerable: false },
    writeText: { value: (value: string) => { textWriter?.write(value); }, enumerable: false },
    hasProgressStream: { value: () => options.progressStream !== undefined, enumerable: false },
    hasTextStream: { value: () => textWriter !== undefined, enumerable: false },
  });
  return call;
}

/** Resolve a credential at call time outside durable Flow state. */
export interface CredentialProvider<C> {
  /** Return current credentials, honoring cancellation. */
  resolve(call: Call, signal?: AbortSignal): Promise<C>;
}

/** Resolve credentials outside a Dex Step, such as for a Trigger source. */
export interface ContextCredentialProvider<C> extends CredentialProvider<C> {
  /** Return current credentials under an explicit cancellation signal. */
  resolveContext(call: Call, signal: AbortSignal): Promise<C>;
}

/** Current private material and decision time for a refresh driver. */
export interface CredentialRefreshState<C> {
  /** Current connector-specific credential. */ credentials: C;
  /** Known access credential expiry. */ expiresAt?: Date;
  /** Captured decision time. */ now: Date;
}

/** Complete replacement credential and mandatory access expiry. */
export interface CredentialRefreshResult<C> {
  /** Complete replacement, retaining omitted renewal material. */ credentials: C;
  /** Replacement access expiry. */ expiresAt: Date;
}

/** Connector-specific provider refresh contract; persistence belongs to the host. */
export interface CredentialRefreshDriver<C> {
  /** Decide deterministically whether a credential needs provider renewal. */
  refreshRequired(state: CredentialRefreshState<C>): boolean;
  /** Make one provider refresh request after the host admits it durably. */
  refresh(state: CredentialRefreshState<C>, signal: AbortSignal): Promise<CredentialRefreshResult<C>>;
}

/** Host adapter that refreshes and persists a connection atomically. */
export interface RefreshingCredentialProvider<C> extends CredentialProvider<C> {
  /** Serialize refresh for one connection, reload state, and persist replacement. */
  resolveWithRefresh(call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C>;
}

/** Host adapter for one explicit provider authentication rejection. */
export interface RejectedCredentialRefreshingProvider<C> {
  /** Force one coordinated refresh after the provider rejects an access token. */
  resolveAfterRejection(call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C>;
}

/** Resolve through host refresh support, then context-aware or ordinary lookup. */
export async function resolveCredential<C>(provider: CredentialProvider<C>, call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C> {
  if (!provider) throw new TypeError('credential provider is required');
  if (!driver) throw new TypeError('credential refresh driver is required');
  if ('resolveWithRefresh' in provider && typeof provider.resolveWithRefresh === 'function') {
    return (provider as RefreshingCredentialProvider<C>).resolveWithRefresh(call, driver, signal);
  }
  if (signal && 'resolveContext' in provider && typeof provider.resolveContext === 'function') {
    return (provider as ContextCredentialProvider<C>).resolveContext(call, signal);
  }
  return provider.resolve(call, signal);
}

/** Force one coordinated refresh after a provider authentication rejection. */
export async function resolveCredentialAfterRejection<C>(provider: CredentialProvider<C>, call: Call, driver: CredentialRefreshDriver<C>, signal?: AbortSignal): Promise<C> {
  if (!provider) throw new TypeError('credential provider is required');
  if (!driver) throw new TypeError('credential refresh driver is required');
  if (!('resolveAfterRejection' in provider) || typeof provider.resolveAfterRejection !== 'function') {
    throw new TypeError('credential provider does not support refresh after rejection');
  }
  return (provider as CredentialProvider<C> & RejectedCredentialRefreshingProvider<C>).resolveAfterRejection(call, driver, signal);
}

/** In-memory named credentials for examples and tests; never persisted by this class. */
export class StaticCredentialProvider<C> implements RefreshingCredentialProvider<C> {
  readonly #credentials: ReadonlyMap<string, C>;

  /** Capture all connection values during construction. */
  constructor(entries: Iterable<readonly [ConnectionRef, C]>) {
    this.#credentials = new Map(Array.from(entries, ([connection, credential]) => [JSON.stringify([connection.provider, connection.name]), credential]));
  }

  /** Return the configured credential or reject a missing connection. */
  async resolve(call: Call): Promise<C> {
    const key = JSON.stringify([call.connection.provider, call.connection.name]);
    if (!this.#credentials.has(key)) throw new Error('connection is not configured');
    return this.#credentials.get(key) as C;
  }

  /** Static credentials never expire and therefore do not call the driver. */
  async resolveWithRefresh(call: Call): Promise<C> { return this.resolve(call); }
}

/** Wrap a secret that cannot be serialized or printed. */
export class SecretString {
  readonly #value: string;

  /** Hold one private value outside default stringification and inspection. */
  constructor(value: string) { this.#value = value; }

  /** Reveal only at the provider request boundary. */
  reveal(): string { return this.#value; }

  /** Return a fixed redaction for accidental string formatting. */
  toString(): string { return '[REDACTED]'; }

  /** Reject accidental JSON persistence. */
  toJSON(): never { throw new TypeError('connector secrets cannot be serialized'); }
}
