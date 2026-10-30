// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CallID, ConnectionRef, OperationRef } from './types.js';

/** Provider-safe call metadata; Dex Context and credentials are excluded. */
export interface Call {
  /** Stable identity across retries. */ id: CallID;
  /** Provider deduplication key, when available. */ idempotencyKey?: string;
  /** Logical named connection. */ connection: ConnectionRef;
  /** Manifest operation identity. */ operation: OperationRef;
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
