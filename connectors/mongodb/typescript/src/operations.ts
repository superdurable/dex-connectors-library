// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { EJSON, BSONError } from 'bson';
import {
  MongoAPIError, MongoClient, MongoError, MongoNetworkError, MongoOperationTimeoutError, MongoParseError, MongoServerError,
  MongoServerSelectionError,
} from 'mongodb';
import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { FindDocumentsDefinition } from './generated/definitions.js';
import type { DocumentSet, FindDocumentsInput } from './generated/models.js';
import type { MongoDBClient } from './client.js';

const emptyDocuments = (): DocumentSet => ({ documents: [] });
const collectionPattern = /^[A-Za-z_][A-Za-z0-9_.-]*$/u;
const javascriptOperators = new Set(['$where', '$function', '$accumulator']);

/** Read bounded canonical Extended JSON documents with MongoDB's native driver. */
export class FindDocumentsOperation implements Query<FindDocumentsInput, DocumentSet> {
  /** Bind one validated client. */ constructor(private readonly client: MongoDBClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return FindDocumentsDefinition; }

  /** Resolve credentials and classify one native find attempt. */
  async invoke(call: Call, input: FindDocumentsInput): Promise<QueryAttempt<DocumentSet>> {
    if (!input || typeof input.collection !== 'string' || !collectionPattern.test(input.collection) ||
        (input.filter !== null && !isObject(input.filter)) || hasJavaScriptOperator(input.filter))
      return branch('defect', 'VALIDATION', 'MongoDB collection or filter is invalid');
    let filter: Record<string, unknown>;
    try {
      const filterJSON = JSON.stringify(input.filter ?? {});
      if (!filterJSON || Buffer.byteLength(filterJSON, 'utf8') > 64 * 1024) throw new TypeError('filter too large');
      const decoded: unknown = EJSON.parse(filterJSON, { relaxed: false });
      if (!isObject(decoded)) throw new TypeError('filter must be an object');
      filter = decoded;
    } catch { return branch('defect', 'VALIDATION', 'MongoDB filter Extended JSON is invalid'); }

    let uri: string;
    try { uri = (await this.client.credentials.resolve(call, call.context.cancellationSignal)).connectionUri.reveal(); }
    catch { return retry('TRANSPORT', 'MongoDB credentials are unavailable'); }
    if (!validConnectionURI(uri)) return branch('defect', 'AUTHENTICATION', 'MongoDB connection URI is invalid');

    const settings = this.client.configuration;
    const startedAt = Date.now();
    let driver: MongoClient;
    try {
      driver = this.client.createDriver(uri, {
        serverSelectionTimeoutMS: settings.timeoutMs, connectTimeoutMS: settings.timeoutMs,
      });
    } catch {
      // The constructor only parses the URI and options, so failures are deterministic; SRV DNS waits for connect().
      return uriRejected();
    }
    try {
      try { await driver.connect(); } catch (error) {
        if (isDNSLookupError(error)) return retry('TRANSPORT', 'MongoDB DNS lookup failed');
        // TXT record contents, SRV host verification, and missing driver options are configuration defects.
        if (error instanceof MongoParseError || error instanceof MongoAPIError) return uriRejected();
        throw error;
      }
      const remainingMs = settings.timeoutMs - (Date.now() - startedAt);
      if (remainingMs <= 0) return retry('TRANSPORT', 'MongoDB read was interrupted');
      const deadline = AbortSignal.timeout(remainingMs);
      const signal = call.context.cancellationSignal
        ? AbortSignal.any([deadline, call.context.cancellationSignal]) : deadline;
      const cursor = driver.db(settings.database).collection(input.collection)
        .find(filter, { signal }).limit(settings.maxDocuments + 1);
      const output: Array<Record<string, unknown>> = [];
      let encodedBytes = 0;
      try {
        for await (const document of cursor) {
          if (output.length >= settings.maxDocuments)
            return branch('truncated', 'RESPONSE_TOO_LARGE', 'MongoDB document count limit was reached', { documents: output });
          let encoded: string;
          let decoded: unknown;
          try {
            encoded = EJSON.stringify(document, { relaxed: false });
            decoded = JSON.parse(encoded) as unknown;
          } catch { return branch('invalidResponse', 'PROTOCOL', 'MongoDB document encoding failed'); }
          if (encoded.includes(uri)) return branch('defect', 'AUTHENTICATION', 'MongoDB response contained a credential');
          if (!isObject(decoded)) return branch('invalidResponse', 'PROTOCOL', 'MongoDB document JSON was invalid');
          encodedBytes += Buffer.byteLength(encoded, 'utf8');
          if (encodedBytes > settings.maxResponseBytes)
            return branch('truncated', 'RESPONSE_TOO_LARGE', 'MongoDB response byte limit was reached', { documents: output });
          output.push(decoded);
        }
      } finally {
        try { await cursor.close(); }
        catch { /* The read decision is fixed; cursor cleanup is best effort. */ }
      }
      return newQueryBranch('found', { documents: output });
    } catch (error) {
      if (error instanceof BSONError) return branch('invalidResponse', 'PROTOCOL', 'MongoDB document encoding failed');
      if (error instanceof MongoNetworkError || error instanceof MongoServerSelectionError || error instanceof MongoOperationTimeoutError ||
          (error instanceof Error && error.name === 'AbortError'))
        return retry('TRANSPORT', 'MongoDB read was interrupted');
      if (error instanceof MongoServerError) {
        if (error.hasErrorLabel('RetryableReadError') || error.code === 50)
          return retry('AVAILABILITY', 'MongoDB read is temporarily unavailable');
        return branch('providerRejected', 'PROVIDER_REJECTION', 'MongoDB rejected the find command');
      }
      if (error instanceof MongoError) return retry('TRANSPORT', 'MongoDB read failed');
      return retry('TRANSPORT', 'MongoDB read failed');
    } finally {
      try { await driver.close(); }
      catch { /* A read outcome is already fixed; closing is best effort. */ }
    }
  }
}

/** Node reports SRV and TXT resolver failures as plain errors identified only by `syscall` and `code`. */
function isDNSLookupError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  const { syscall, code } = error as Error & { syscall?: unknown; code?: unknown };
  return (syscall === 'querySrv' || syscall === 'queryTxt') && typeof code === 'string';
}
function uriRejected(): QueryAttempt<DocumentSet> {
  return branch('defect', 'VALIDATION', 'MongoDB connection URI was rejected');
}
function validConnectionURI(uri: string): boolean {
  return uri.length <= 4096 && !uri.includes('\0') && !uri.includes('\r') && !uri.includes('\n') &&
    (uri.startsWith('mongodb://') || uri.startsWith('mongodb+srv://'));
}
function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function hasJavaScriptOperator(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(hasJavaScriptOperator);
  if (isObject(value)) return Object.entries(value).some(([key, child]) => javascriptOperators.has(key) || hasJavaScriptOperator(child));
  return false;
}
function branch(branchId: string, kind: Failure['kind'], message: string, output: DocumentSet = emptyDocuments()): QueryAttempt<DocumentSet> {
  return newQueryBranch(branchId, output, {}, { kind, provider: 'mongodb', operation: 'findDocuments', message });
}
function retry(kind: Failure['kind'], message: string): QueryAttempt<DocumentSet> {
  return newQueryRetry({ kind, provider: 'mongodb', operation: 'findDocuments', message });
}
