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
