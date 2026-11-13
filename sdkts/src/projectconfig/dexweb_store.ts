// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID, createHash } from 'node:crypto';
import { lstat, mkdir, open, readdir, readFile, rename, unlink } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { ConnectionRecordSchema } from './connection_schema.js';
import { ConnectionStore, CredentialMaterial, type ConnectionKey } from './connections.js';
import { LocalConnectionsFileSchema, type LocalConnection, type Scope } from './model.js';
import { z } from 'zod';
import { ConflictError, InMemoryObjectStore, ObjectNotFoundError, ProjectObject, type ObjectStore, encodeGoCompatibleJSON, localObjectByteLimit, validateContents, validateObjectKey } from './storage.js';

/** Process-local Dex Web credential projection with guarded owner-only writeback. */
export class DexWebObjectStore implements ObjectStore {
  readonly #memory = new InMemoryObjectStore();
  readonly #inboxes: LocalFileObjectStore;
  readonly #connections: ConnectionStore;
  readonly #imported = new Map<string, string>();
  #lockTail: Promise<void> = Promise.resolve();

  /** Bind a mode-0600 connections.json file and local project scope; Trigger inboxes persist beside it in trigger-inboxes. */
  constructor(private readonly path: string, scope: Scope) {
    this.#connections = new ConnectionStore(this.#memory, scope);
    this.#inboxes = new LocalFileObjectStore(join(dirname(path), 'trigger-inboxes'));
  }

  /** Import current Dex Web credentials and load persisted Trigger inboxes before application registration. */
  async initialize(): Promise<void> {
    await this.#inboxes.initialize();
    await this.#withLock(() => this.#sync());
  }

  /** Adopt Dex Web updates before reading a connection head. */
  async readObject(key: string, version = ''): Promise<ProjectObject> {
    if (isTriggerInboxKey(key)) return this.#inboxes.readObject(key, version);
    if (isConnectionHead(key)) return this.#withLock(async () => { await this.#sync(); return this.#memory.readObject(key, version); });
    return this.#memory.readObject(key, version);
  }

  /** Create an immutable private object in process memory, or a Trigger inbox object on disk. */
  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> {
    return isTriggerInboxKey(key) ? this.#inboxes.createObject(key, contents) : this.#memory.createObject(key, contents);
  }

  /** CAS a head, then write back a refreshed credential if the file is still current. */
  async compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject> {
    if (isTriggerInboxKey(key)) return this.#inboxes.compareAndSwapObject(key, expectedETag, contents);
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

/**
 * Versioned objects of one local application, kept in memory and as private files in directory, so a
 * restarted application finds its Trigger inboxes again. Files match the Go Connector SDK's local store:
 * "<sha256 of key>-<version>.json" holding the key, version, ETag, and base64 contents.
 */
class LocalFileObjectStore implements ObjectStore {
  readonly #objects = new Map<string, ProjectObject>();
  #tail: Promise<void> = Promise.resolve();

  constructor(private readonly directory: string) {}

  async initialize(): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const loaded = new Map<string, ProjectObject[]>();
    for (const name of await readdir(this.directory)) {
      if (!name.endsWith('.json')) continue;
      const stored = StoredLocalObjectSchema.safeParse(parseJSON(await readFile(join(this.directory, name))));
      if (!stored.success) throw new Error('local project storage file is invalid');
      const object = new ProjectObject(stored.data.key, stored.data.version, stored.data.etag, Buffer.from(stored.data.contents, 'base64'));
      loaded.set(object.key, [...(loaded.get(object.key) ?? []), object]);
    }
    for (const [key, versions] of loaded) {
      versions.sort((left, right) => Number(left.version) - Number(right.version));
      // A crash between writing a new version and deleting the old one leaves both.
      for (const stale of versions.slice(0, -1)) await this.#remove(stale);
      this.#objects.set(key, versions.at(-1)!);
    }
  }

  async readObject(key: string, version = ''): Promise<ProjectObject> {
    const object = this.#objects.get(key);
    if (!object || (version && object.version !== version)) throw new ObjectNotFoundError();
    return object;
  }

  async createObject(key: string, contents: Uint8Array): Promise<ProjectObject> {
    return this.#serialize(async () => {
      if (this.#objects.has(key)) throw new ConflictError();
      return this.#write(key, contents, undefined);
    });
  }

  async compareAndSwapObject(key: string, expectedETag: string, contents: Uint8Array): Promise<ProjectObject> {
    return this.#serialize(async () => {
      const current = this.#objects.get(key);
      if (!current) throw new ObjectNotFoundError();
      if (current.etag !== expectedETag) throw new ConflictError();
      return this.#write(key, contents, current);
    });
  }

  /** Make the new version durable before deleting the superseded file, so a crash never loses the key. */
  async #write(key: string, contents: Uint8Array, previous: ProjectObject | undefined): Promise<ProjectObject> {
    validateObjectKey(key);
    validateContents(contents, localObjectByteLimit(key));
    const version = String(previous ? Number(previous.version) + 1 : 1);
    const object = new ProjectObject(key, version, `sha256:${createHash('sha256').update(contents).digest('hex')}-${version}`, new Uint8Array(contents));
    await writePrivateFile(this.#file(object), Buffer.from(JSON.stringify({ key, version, etag: object.etag, contents: Buffer.from(contents).toString('base64') })));
    this.#objects.set(key, object);
    if (previous) await this.#remove(previous);
    return object;
  }

  async #remove(object: ProjectObject): Promise<void> {
    // A failure leaves a stale file that the next load discards.
    await unlink(this.#file(object)).catch(() => undefined);
  }

  #file(object: ProjectObject): string { return join(this.directory, `${createHash('sha256').update(object.key).digest('hex')}-${object.version}.json`); }

  #serialize<T>(action: () => Promise<T>): Promise<T> {
    const result = this.#tail.then(action);
    this.#tail = result.then(() => undefined, () => undefined);
    return result;
  }
}

const StoredLocalObjectSchema = z.object({ key: z.string().min(1), version: z.string().regex(/^[1-9][0-9]*$/), etag: z.string().min(1), contents: z.string() });

function isTriggerInboxKey(key: string): boolean { return key.includes('/trigger-inboxes/'); }
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
  await syncDirectory(dirname(path));
}

/**
 * Make a rename in directory durable, as the Go SDK does after replacing a private file, so an acknowledged
 * Trigger inbox or refreshed credential survives power loss. Windows cannot open a directory for this, and
 * the Go SDK skips it there too.
 */
async function syncDirectory(directory: string): Promise<void> {
  if (process.platform === 'win32') return;
  const handle = await open(directory, 'r');
  try { await handle.sync(); }
  finally { await handle.close(); }
}
