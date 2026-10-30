// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError } from '../errors.js';
import { ConnectionNotSelectedError, CredentialBrokerUnavailableError, RejectedCredentialNotRefreshedError, type BrokerCredential, type CredentialBrokerClient } from './broker.js';
import type { ConnectionKey } from './connections.js';
import type { Configuration } from './model.js';

interface SharedEntry {
  id: string;
  cached: BrokerCredential | undefined;
  readAt: number;
  notBefore: number;
  issuedByCall: Map<string, number>;
  pending: Promise<BrokerCredential> | undefined;
}

/** Broker-backed shared selections fixed by the loaded project configuration. */
export class SharedConnections {
  readonly #entries = new Map<string, SharedEntry>();

  /** Bind shared entries to the project's trusted credential broker. */
  constructor(configuration: Configuration, private readonly broker: Pick<CredentialBrokerClient, 'read'>) {
    for (const connection of configuration.connections) {
      if (connection.sharedConnectionId) this.#entries.set(keyString(connection), {
        id: connection.sharedConnectionId, cached: undefined, readAt: 0, notBefore: 0, issuedByCall: new Map(), pending: undefined,
      });
    }
  }

  /** Whether a loaded connection resolves through the broker. */
  has(key: ConnectionKey): boolean { return this.#entries.has(keyString(key)); }

  /** Read a current shared credential and remember the revision issued to this call. */
  async read(key: ConnectionKey, callId: string, signal?: AbortSignal): Promise<BrokerCredential> {
    const entry = this.#entry(key);
    const now = Date.now();
    let credential: BrokerCredential;
    if (entry.cached && isFresh(entry, now)) credential = entry.cached;
    else if (entry.pending && entry.cached && isUnexpired(entry.cached, now)) credential = entry.cached;
    else {
      try { credential = await this.#readBroker(entry, 0, signal); }
      catch (error) {
        if (error instanceof CredentialBrokerUnavailableError && entry.cached && isUnexpired(entry.cached, Date.now())) {
          entry.notBefore = Date.now() + 10_000;
          credential = entry.cached;
        } else throw error;
      }
    }
    rememberIssued(entry, callId, credential.revision);
    return credential;
  }

  /** Ask the broker for a revision newer than the one issued to this call. */
  async readAfterRejection(key: ConnectionKey, callId: string, signal?: AbortSignal): Promise<BrokerCredential> {
    const entry = this.#entry(key);
    const issued = entry.issuedByCall.get(callId) ?? 0;
    if (entry.pending) await entry.pending;
    if (entry.cached && entry.cached.revision > issued && isUnexpired(entry.cached, Date.now())) {
      rememberIssued(entry, callId, entry.cached.revision);
      return entry.cached;
    }
    const credential = await this.#readBroker(entry, issued, signal);
    if (issued > 0 && credential.revision === issued) throw new RejectedCredentialNotRefreshedError();
    rememberIssued(entry, callId, credential.revision);
    return credential;
  }

  #entry(key: ConnectionKey): SharedEntry {
    const entry = this.#entries.get(keyString(key));
    if (!entry) throw new TypeError('connection is not a loaded shared selection');
    return entry;
  }

  async #readBroker(entry: SharedEntry, rejectedRevision: number, signal?: AbortSignal): Promise<BrokerCredential> {
    if (entry.pending) return entry.pending;
    const pending = this.broker.read(entry.id, rejectedRevision, signal).then(credential => {
      entry.cached = credential;
      entry.readAt = Date.now();
      entry.notBefore = entry.readAt + 1000;
      return credential;
    }, error => {
      if (error instanceof ReauthorizationRequiredError || error instanceof ConnectionNotSelectedError) entry.cached = undefined;
      throw error;
    });
    entry.pending = pending;
    try { return await pending; }
    finally { if (entry.pending === pending) entry.pending = undefined; }
  }
}

function keyString(key: ConnectionKey): string { return `${key.connectorId}\0${key.connectionName}`; }

function isUnexpired(credential: BrokerCredential, now: number): boolean { return !credential.expiresAt || Date.parse(credential.expiresAt) > now; }

function isFresh(entry: SharedEntry, now: number): boolean {
  const credential = entry.cached;
  if (!credential || !isUnexpired(credential, now)) return false;
  if (now < entry.notBefore) return true;
  if (!credential.expiresAt) return now - entry.readAt < 15 * 60_000;
  const lifetime = credential.issuedAt ? Date.parse(credential.expiresAt) - Date.parse(credential.issuedAt) : 0;
  const skew = lifetime > 0 ? Math.min(5 * 60_000, lifetime / 4) : 5 * 60_000;
  return Date.parse(credential.expiresAt) > now + skew;
}

function rememberIssued(entry: SharedEntry, callId: string, revision: number): void {
  entry.issuedByCall.set(callId, revision);
  if (entry.issuedByCall.size > 4096) entry.issuedByCall.delete(entry.issuedByCall.keys().next().value!);
}
