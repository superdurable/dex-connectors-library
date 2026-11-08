// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import {
  newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry,
  type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import { DownloadFileDefinition, UploadFileDefinition } from './generated/definitions.js';
import { emptyDownloadedFile, emptyUploadedFile } from './generated/mock.js';
import {
  DownloadFileInputSchema, UploadFileInputSchema,
  type DownloadedFile, type DownloadFileInput, type UploadedFile, type UploadFileInput,
} from './models.js';
import type { BoxClient, BoxExchange } from './client.js';

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'box', operation, message };
}
function downloadBranch(branch: string, kind: Failure['kind'], message: string): QueryAttempt<DownloadedFile> {
  return newQueryBranch(branch, emptyDownloadedFile, {}, failure('downloadFile', kind, message));
}
function downloadRetry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<DownloadedFile> {
  return newQueryRetry(failure('downloadFile', kind, message), delay);
}
function uploadBranch(branch: string, kind: Failure['kind'], message: string): MutationAttempt<UploadedFile> {
  return newMutationBranch(branch, emptyUploadedFile, {}, failure('uploadFile', kind, message));
}
function uploadRetry(kind: Failure['kind'], message: string, delay = 0): MutationAttempt<UploadedFile> {
  return newMutationRetry(failure('uploadFile', kind, message), delay);
}
function uncertainUpload(): MutationAttempt<UploadedFile> {
  return newMutationUncertain(emptyUploadedFile,
    failure('uploadFile', 'TRANSPORT', 'Box may have replaced the file but its current content could not be confirmed'));
}
function validFileID(value: string): boolean { return /^[0-9]{1,40}$/.test(value); }

interface FileMetadata { fileID: string; kind: string; name: string; sha1: string; size: number }

function decodeMetadata(body: Uint8Array, fileID: string): FileMetadata {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Box file metadata is not an object');
  const record = value as Record<string, unknown>;
  const { id, type, name, sha1, size } = record;
  if (id !== fileID || type !== 'file' || typeof name !== 'string' || !name
    || typeof sha1 !== 'string' || !/^[0-9a-fA-F]{40}$/.test(sha1)
    || typeof size !== 'number' || !Number.isSafeInteger(size) || size < 0)
    throw new TypeError('Box file metadata is incomplete');
  return { fileID, kind: type, name, sha1, size };
}
function matches(metadata: FileMetadata, fileID: string, digest: string, size: number): boolean {
  return metadata.fileID === fileID && metadata.kind === 'file' && !!metadata.name
    && metadata.sha1.toUpperCase() === digest && metadata.size === size;
}
function uploaded(metadata: FileMetadata): MutationAttempt<UploadedFile> {
  return newMutationBranch('uploaded', { fileId: metadata.fileID, name: metadata.name, sha1: metadata.sha1.toUpperCase() });
}

/** One bounded Box file-content Query. */
export class DownloadFileOperation implements Query<DownloadFileInput, DownloadedFile> {
  /** Bind the provider client. */ constructor(private readonly client: BoxClient) {
    if (!client) throw new TypeError('Box client is required');
  }
  /** Return stable manifest metadata. */ definition() { return DownloadFileDefinition; }
  /** Download bytes, classify provider responses, and encode bounded content. */
  async invoke(call: Call, input: DownloadFileInput): Promise<QueryAttempt<DownloadedFile>> {
    input = DownloadFileInputSchema.parse(input);
    if (!validFileID(input.fileId)) return downloadBranch('defect', 'VALIDATION', 'Box file ID must contain only decimal digits');
    const exchange = await this.client.download(call, input.fileId);
    if (exchange.error === 'credential') return downloadBranch('defect', 'AUTHENTICATION', 'Box credentials are unavailable or invalid');
    if (exchange.error === 'refresh') return downloadRetry('AVAILABILITY', 'Box credentials could not be refreshed');
    if (exchange.error === 'request') return downloadRetry('TRANSPORT', 'Box download request failed');
    if (exchange.status === 429) return downloadRetry('RATE_LIMIT', 'Box rate limited the download', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return downloadRetry('AVAILABILITY', 'Box download is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200 && exchange.error === 'tooLarge')
      return downloadBranch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Box error response exceeds the configured limit');
    if (exchange.status === 404) return downloadBranch('notFound', 'NOT_FOUND', 'Box did not find the file');
    if (exchange.status !== 200) {
      if (exchange.error === 'invalidRedirect') return downloadBranch('invalidResponse', 'PROTOCOL', 'Box returned an unsafe download redirect');
      return downloadBranch('providerRejected', 'PROVIDER_REJECTION', `Box rejected HTTP ${exchange.status}`);
    }
    if (exchange.error === 'tooLarge') return downloadBranch('tooLarge', 'RESPONSE_TOO_LARGE', 'Box file exceeds the configured download limit');
    if (exchange.error) return downloadRetry('TRANSPORT', 'Box download response was interrupted');
    return newQueryBranch('downloaded', { fileId: input.fileId, contentBase64: Buffer.from(exchange.body).toString('base64'), contentBytes: exchange.body.length });
  }
}

/** One bounded Box file-version replacement Mutation. */
export class UploadFileOperation implements Mutation<UploadFileInput, UploadedFile> {
  /** Bind the provider client. */ constructor(private readonly client: BoxClient) {
    if (!client) throw new TypeError('Box client is required');
  }
  /** Return stable manifest metadata. */ definition() { return UploadFileDefinition; }
  /** Record stable call identity; Box receives no request key. */ idempotencyKey(callID: string, _input: UploadFileInput): string {
    void _input;
    return callID;
  }
  /** Compare current content, replace once, and read back ambiguous outcomes. */
  async invoke(call: Call, input: UploadFileInput): Promise<MutationAttempt<UploadedFile>> {
    input = UploadFileInputSchema.parse(input);
    if (!validFileID(input.fileId)) return uploadBranch('defect', 'VALIDATION', 'Box file ID must contain only decimal digits');
    if (Math.ceil(input.contentBase64.length / 4) * 3 > this.client.maxUploadBytes)
      return uploadBranch('defect', 'VALIDATION', 'Box upload exceeds the configured byte limit');
    const content = Buffer.from(input.contentBase64, 'base64');
    if (content.toString('base64') !== input.contentBase64 || content.length > this.client.maxUploadBytes)
      return uploadBranch('defect', 'VALIDATION', 'Box upload content must be bounded Base64');
    const digest = createHash('sha1').update(content).digest('hex').toUpperCase();
    const first = await this.client.getFileMetadata(call, input.fileId);
    const initial = initialMetadata(first, input.fileId);
    if ('decision' in initial) return initial.decision;
    const metadata = initial.metadata;
    if (matches(metadata, input.fileId, digest, content.length)) return uploaded(metadata);
    const exchange = await this.client.upload(call, input.fileId, metadata.name, content);
    if (exchange.error === 'credential' && !exchange.requestSent)
      return uploadBranch('defect', 'AUTHENTICATION', 'Box credentials are unavailable or invalid');
    if (exchange.error === 'refresh') return uploadRetry('AVAILABILITY', 'Box credentials could not be refreshed');
    if (exchange.error === 'request' && !exchange.requestSent)
      return uploadRetry('TRANSPORT', 'Box upload did not leave the process');
    if (exchange.status === 429) return uploadRetry('RATE_LIMIT', 'Box rate limited the upload', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500)
      return uploadRetry('AVAILABILITY', 'Box upload is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return uploadBranch('notFound', 'NOT_FOUND', 'Box did not find the file');
    if (exchange.status !== 0 && exchange.status !== 201)
      return uploadBranch('providerRejected', 'PROVIDER_REJECTION', `Box rejected HTTP ${exchange.status}`);
    if (exchange.status === 201 && !exchange.error) {
      try {
        const wire: unknown = JSON.parse(Buffer.from(exchange.body).toString('utf8'));
        if (wire && typeof wire === 'object' && 'entries' in wire && Array.isArray(wire.entries) && wire.entries.length === 1) {
          const returned = decodeMetadata(Buffer.from(JSON.stringify(wire.entries[0])), input.fileId);
          if (matches(returned, input.fileId, digest, content.length)) return uploaded(returned);
        }
      } catch { /* Read-back resolves malformed or missing provider confirmation. */ }
    }
    const readback = await this.client.getFileMetadata(call, input.fileId);
    if (readback.status === 200 && !readback.error) {
      try {
        const confirmed = decodeMetadata(readback.body, input.fileId);
        if (matches(confirmed, input.fileId, digest, content.length)) return uploaded(confirmed);
      } catch { /* The outcome remains uncertain when read-back cannot validate it. */ }
    }
    return uncertainUpload();
  }
}

function initialMetadata(exchange: BoxExchange, fileID: string): { metadata: FileMetadata } | { decision: MutationAttempt<UploadedFile> } {
  if (exchange.error === 'credential') return { decision: uploadBranch('defect', 'AUTHENTICATION', 'Box credentials are unavailable or invalid') };
  if (exchange.error === 'refresh') return { decision: uploadRetry('AVAILABILITY', 'Box credentials could not be refreshed') };
  if (exchange.error === 'request') return { decision: uploadRetry('TRANSPORT', 'Box metadata request failed') };
  if (exchange.status === 429) return { decision: uploadRetry('RATE_LIMIT', 'Box rate limited metadata lookup', exchange.retryAfterMs) };
  if (exchange.status === 408 || exchange.status >= 500)
    return { decision: uploadRetry('AVAILABILITY', 'Box metadata is temporarily unavailable', exchange.retryAfterMs) };
  if (exchange.status === 404) return { decision: uploadBranch('notFound', 'NOT_FOUND', 'Box did not find the file') };
  if (exchange.status !== 200)
    return { decision: uploadBranch('providerRejected', 'PROVIDER_REJECTION', `Box rejected HTTP ${exchange.status}`) };
  if (exchange.error === 'tooLarge')
    return { decision: uploadBranch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Box metadata exceeds the configured limit') };
  if (exchange.error) return { decision: uploadRetry('TRANSPORT', 'Box metadata response was interrupted') };
  try { return { metadata: decodeMetadata(exchange.body, fileID) }; }
  catch { return { decision: uploadBranch('invalidResponse', 'PROTOCOL', 'Box returned invalid file metadata') }; }
}
