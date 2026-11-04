// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { CohereClient } from './client.js';
import { EmbedDefinition, RerankDefinition } from './generated/definitions.js';
import type { EmbedInput, EmbedOutput, RankedDocument, RerankInput, RerankOutput } from './generated/models.js';

const emptyEmbed: EmbedOutput = { id: '', embeddings: [] };
const emptyRerank: RerankOutput = { results: [] };
const allowedInputTypes = new Set(['search_document', 'search_query', 'classification', 'clustering']);

/** Cohere v2 text embedding query. */
export class EmbedOperation implements Query<EmbedInput, EmbedOutput> {
  /** Bind an immutable client. */ constructor(private readonly client: CohereClient) { if (!client) throw new TypeError('Cohere client is required'); }
  /** Return manifest identity and retry policy. */ definition() { return EmbedDefinition; }
  /** Validate input and request one float vector per text. */
  async invoke(call: Call, input: EmbedInput): Promise<QueryAttempt<EmbedOutput>> {
    if (typeof input?.model !== 'string' || !input.model.trim() || !Array.isArray(input.texts) || input.texts.length < 1 || input.texts.length > 96) {
      return defect('embed', emptyEmbed, 'model and 1 to 96 texts are required');
    }
    if (input.texts.some(text => typeof text !== 'string' || !text.trim())) return defect('embed', emptyEmbed, 'texts must be nonempty');
    if (!allowedInputTypes.has(input.input_type)) return defect('embed', emptyEmbed, 'unsupported input_type');
    return this.client.invokeJSON(call, 'embed', '/v2/embed', {
      model: input.model, texts: input.texts, input_type: input.input_type, embedding_types: ['float'],
    }, 'embedded', emptyEmbed, body => decodeEmbed(body, input.texts!.length));
  }
}

/** Cohere v2 document relevance query. */
export class RerankOperation implements Query<RerankInput, RerankOutput> {
  /** Bind an immutable client. */ constructor(private readonly client: CohereClient) { if (!client) throw new TypeError('Cohere client is required'); }
  /** Return manifest identity and retry policy. */ definition() { return RerankDefinition; }
  /** Validate input and rank document indices by relevance. */
  async invoke(call: Call, input: RerankInput): Promise<QueryAttempt<RerankOutput>> {
    if (typeof input?.model !== 'string' || !input.model.trim() || typeof input.query !== 'string' || !input.query.trim() || !Array.isArray(input.documents) || input.documents.length < 1 || input.documents.length > 1000 || !Number.isInteger(input.top_n ?? 0) || (input.top_n ?? 0) < 0 || (input.top_n ?? 0) > input.documents.length) {
      return defect('rerank', emptyRerank, 'model, query, 1 to 1000 documents, and valid topN are required');
    }
    if (input.documents.some(document => typeof document !== 'string' || !document.trim())) return defect('rerank', emptyRerank, 'documents must be nonempty');
    return this.client.invokeJSON(call, 'rerank', '/v2/rerank', {
      model: input.model, query: input.query, documents: input.documents, ...(input.top_n ? { top_n: input.top_n } : {}),
    }, 'ranked', emptyRerank, body => decodeRerank(body, input.documents!.length));
  }
}

function decodeEmbed(body: unknown, expectedCount: number): EmbedOutput {
  const record = asRecord(body);
  const embeddings = asRecord(record.embeddings).float;
  if (typeof record.id !== 'string' || !record.id || !Array.isArray(embeddings) || embeddings.length !== expectedCount) throw new TypeError('incomplete embeddings');
  if (embeddings.some(vector => !Array.isArray(vector) || vector.length === 0 || vector.some(value => value !== null && (typeof value !== 'number' || !Number.isFinite(value))))) throw new TypeError('invalid embedding');
  return { id: record.id, embeddings: embeddings.map(vector => (vector as Array<number | null>).map(value => value ?? 0)) };
}

function decodeRerank(body: unknown, documentCount: number): RerankOutput {
  const results = asRecord(body).results;
  if (!Array.isArray(results)) throw new TypeError('missing ranking');
  const seen = new Set<number>();
  const ranked: RankedDocument[] = [];
  for (const entry of results) {
    const value = asRecord(entry);
    const index = value.index ?? 0;
    const score = value.relevance_score ?? 0;
    if (typeof index !== 'number' || !Number.isInteger(index) || typeof score !== 'number' || index < 0 || index >= documentCount || seen.has(index)) throw new TypeError('invalid ranking');
    seen.add(index);
    ranked.push({ index, relevanceScore: score });
  }
  return { results: ranked };
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected object');
  return value as Record<string, unknown>;
}

function defect<Output>(operation: string, empty: Output, message: string): QueryAttempt<Output> {
  const failure: Failure = { kind: 'VALIDATION', provider: 'cohere', operation, message };
  return newQueryBranch('defect', empty, zeroReceipt(), failure);
}
