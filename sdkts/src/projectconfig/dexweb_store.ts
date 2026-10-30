// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID, createHash } from 'node:crypto';
import { lstat, open, readFile, rename, unlink } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { ConnectionRecordSchema } from './connection_schema.js';
import { ConnectionStore, CredentialMaterial, type ConnectionKey } from './connections.js';
import { LocalConnectionsFileSchema, type LocalConnection, type Scope } from './model.js';
import { InMemoryObjectStore, ObjectNotFoundError, type ObjectStore, type ProjectObject, encodeGoCompatibleJSON } from './storage.js';

/** Process-local Dex Web credential projection with guarded owner-only writeback. */
export class DexWebObjectStore implements ObjectStore {
  readonly #memory = new InMemoryObjectStore();
  readonly #connections: ConnectionStore;
  readonly #imported = new Map<string, string>();
  #lockTail: Promise<void> = Promise.resolve();

  /** Bind a mode-0600 connections.json file and local project scope. */
  constructor(private readonly path: string, scope: Scope) { this.#connections = new ConnectionStore(this.#memory, scope); }

  /** Import current Dex Web credentials before application registration. */
  async initialize(): Promise<void> { await this.#withLock(() => this.#sync()); }

  /** Adopt Dex Web updates before reading a connection head. */
  async readObject(key: string, version = ''): Promise<ProjectObject> {
    if (isConnectionHead(key)) return this.#withLock(async () => { await this.#sync(); return this.#memory.readObject(key, version); });
    return this.#memory.readObject(key, version);
  }

  /** Create an immutable private object in process memory. */
  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> { return this.#memory.createObject(key, contents); }

  /** CAS a head, then write back a refreshed credential if the file is still current. */
  async compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject> {
    if (!isConnectionHead(key)) return this.#memory.compareAndSwapObject(key, expectedETag, contents);
    return this.#withLock(async () => {
      await this.#sync();
      const object = await this.#memory.compareAndSwapObject(key, expectedETag, contents);
      await this.#writeBack(object);
      return object;
    });
  }

  async #sync(): Promise<void> {
    const file = await readDexWebFile(this.path);
    for (const connection of file.connections) {
      const key = { connectorId: connection.connectorId, connectionName: connection.connectionName };
      const expiresAt = connection.credentialExpiresAt ? new Date(connection.credentialExpiresAt) : undefined;
      const authMethod = connection.authMethodId || (connection.authMethodIds?.length === 1 ? connection.authMethodIds[0]! : 'local');
      const material = new CredentialMaterial(connection.credentials, expiresAt, authMethod);
      const digest = credentialDigest(connection.credentials, connection.credentialExpiresAt);
      if (this.#imported.get(keyString(key)) === digest) continue;
      let revision = 0;
      try { revision = (await this.#connections.readConnection(key)).revision; }
      catch (error) { if (!(error instanceof ObjectNotFoundError)) throw error; }
      await this.#connections.replaceCredential(key, revision, material);
      this.#imported.set(keyString(key), digest);
    }
  }

  async #writeBack(head: ProjectObject): Promise<void> {
    const parsed = ConnectionRecordSchema.safeParse(parseJSON(head.contents));
    if (!parsed.success || parsed.data.status !== 'READY' || !parsed.data.credential) return;
    const key = parsed.data.connection;
    try {
      const material = await this.#connections.readMaterial(key, parsed.data);
      const digest = credentialDigest(material.credentials, material.expiresAt?.toISOString());
      if (digest === this.#imported.get(keyString(key))) return;
      await this.#replaceInFile(key, material);
      this.#imported.set(keyString(key), digest);
    } catch {
      // The durable in-memory head already committed; Dex Web can supply a newer credential on next read.
      console.warn('local connector credential write-back skipped');
    }
  }

  async #replaceInFile(key: ConnectionKey, material: CredentialMaterial): Promise<void> {
    const file = await readDexWebFile(this.path);
    const record = file.connections.find(connection => connection.connectorId === key.connectorId && connection.connectionName === key.connectionName);
    if (!record) throw new Error('the connection is no longer in the file');
    if (credentialDigest(record.credentials, record.credentialExpiresAt) !== this.#imported.get(keyString(key))) throw new Error('the file holds a newer credential');
    record.credentials = material.credentials;
    if (material.expiresAt) record.credentialExpiresAt = material.expiresAt.toISOString();
    else delete record.credentialExpiresAt;
    await writePrivateFile(this.path, Buffer.from(`${JSON.stringify(file, null, 2)}\n`));
  }

  async #withLock<T>(action: () => Promise<T>): Promise<T> {
    const previous = this.#lockTail;
    let release!: () => void;
    this.#lockTail = new Promise<void>(resolve => { release = resolve; });
    await previous;
    try { return await action(); }
    finally { release(); }
  }
}

function isConnectionHead(key: string): boolean { return key.includes('/connections/') && key.endsWith('/head'); }
function keyString(key: ConnectionKey): string { return `${key.connectorId}\0${key.connectionName}`; }

function credentialDigest(credentials: Record<string, unknown>, expiresAt?: string | null): string {
  const canonical = encodeGoCompatibleJSON(credentials);
  const expiry = expiresAt ? (JSON.parse(encodeGoCompatibleJSON({ expiry: expiresAt })) as { expiry: string }).expiry : '';
  return createHash('sha256').update(`${canonical}${expiry ? `|${expiry}` : ''}`).digest('hex');
}

interface RawDexWebFile extends Record<string, unknown> { connections: Array<LocalConnection & Record<string, unknown>> }

async function readDexWebFile(path: string): Promise<RawDexWebFile> {
  const stat = await lstat(path);
  if (!stat.isFile() || stat.isSymbolicLink() || (stat.mode & 0o777) !== 0o600) throw new Error('connector configuration file must be a regular mode-0600 file');
  let raw: unknown;
  try { raw = JSON.parse(await readFile(path, 'utf8')) as unknown; }
  catch { throw new Error('connector configuration file is not valid JSON or not private'); }
  const validated = LocalConnectionsFileSchema.safeParse(raw);
  if (!validated.success || !raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('connector configuration file violates its schema');
  return raw as RawDexWebFile;
}

function parseJSON(contents: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(contents).toString('utf8')) as unknown; }
  catch { return undefined; }
}

async function writePrivateFile(path: string, contents: Uint8Array): Promise<void> {
  const temporary = join(dirname(path), `.write-${randomUUID()}`);
  const handle = await open(temporary, 'wx', 0o600);
  let isClosed = false;
  try {
    await handle.writeFile(contents);
    await handle.sync();
    await handle.close();
    isClosed = true;
    await rename(temporary, path);
  } finally {
    if (!isClosed) await handle.close();
    await unlink(temporary).catch(() => undefined);
  }
}
