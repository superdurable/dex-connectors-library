// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { PineconeClient, PineconeExchangeFailure } from './client.js';
import { QueryVectorsDefinition, UpsertVectorsDefinition } from './generated/definitions.js';
import type { QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput } from './generated/models.js';

const emptyUpsert: UpsertVectorsOutput = { upsertedCount: 0 };
const emptyQuery: QueryVectorsOutput = { data: null };

/** Replace vector IDs in one namespace using a repeat-safe mutation. */
export class UpsertVectorsOperation implements Mutation<UpsertVectorsInput, UpsertVectorsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: PineconeClient) {}
  /** Return the manifest contract. */ definition() { return UpsertVectorsDefinition; }
  /** The absolute replacement can safely reuse the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and classify one upsert attempt. */
  async invoke(call: Call, input: UpsertVectorsInput): Promise<MutationAttempt<UpsertVectorsOutput>> {
    const vectors = input.vectors;
    if (!vectors || vectors.length < 1 || vectors.length > 1000) return mutationFailure('defect', 'VALIDATION', 'vectors must contain 1 through 1000 items');
    const ids = new Set<string>();
    for (const vector of vectors) {
      if (!vector.id || !vector.values?.length) return mutationFailure('defect', 'VALIDATION', 'each vector needs an ID and values');
      if (ids.has(vector.id)) return mutationFailure('defect', 'VALIDATION', 'vector IDs must be unique within the batch');
      ids.add(vector.id);
    }
    const requestBody = {
      ...(input.namespace ? { namespace: input.namespace } : {}),
      vectors: vectors.map(vector => ({
        id: vector.id, values: vector.values,
        ...(vector.metadata && Object.keys(vector.metadata).length ? { metadata: vector.metadata } : {}),
      })),
    };
    const result = await this.client.exchange(call, 'upsertVectors', '/vectors/upsert', requestBody);
    if ('outcome' in result) return mutationOutcome(result.outcome);
    if (!isObject(result.body) || !('upsertedCount' in result.body) || !Number.isInteger(result.body.upsertedCount) || (result.body.upsertedCount as number) < 0 || (result.body.upsertedCount as number) > vectors.length) {
      return mutationFailure('invalidResponse', 'PROTOCOL', 'provider returned an invalid count');
    }
    return newMutationBranch('upserted', { upsertedCount: result.body.upsertedCount as number }, {});
  }
}

/** Find nearest vectors in one namespace. */
export class QueryVectorsOperation implements Query<QueryVectorsInput, QueryVectorsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: PineconeClient) {}
  /** Return the manifest contract. */ definition() { return QueryVectorsDefinition; }
  /** Validate, send, and classify one vector query. */
  async invoke(call: Call, input: QueryVectorsInput): Promise<QueryAttempt<QueryVectorsOutput>> {
    if (!input.vector?.length || !Number.isInteger(input.topK) || input.topK < 1 || input.topK > 10_000) return queryFailure('defect', 'VALIDATION', 'vector and topK from 1 through 10000 are required');
    const requestBody = {
      ...(input.namespace ? { namespace: input.namespace } : {}),
      vector: input.vector, topK: input.topK,
      ...(input.includeMetadata ? { includeMetadata: true } : {}),
    };
    const result = await this.client.exchange(call, 'queryVectors', '/query', requestBody);
    if ('outcome' in result) return queryOutcome(result.outcome);
    if (!isObject(result.body) || !Array.isArray(result.body.matches)) return queryFailure('invalidResponse', 'PROTOCOL', 'provider returned an incomplete response');
    return newQueryBranch('queried', { data: result.body }, {});
  }
}

function mutationOutcome(outcome: PineconeExchangeFailure): MutationAttempt<UpsertVectorsOutput> {
  if (outcome.disposition === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch(outcome.disposition, emptyUpsert, {}, outcome.failure);
}
function queryOutcome(outcome: PineconeExchangeFailure): QueryAttempt<QueryVectorsOutput> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.disposition, emptyQuery, {}, outcome.failure);
}
function mutationFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<UpsertVectorsOutput> {
  return newMutationBranch(branch, emptyUpsert, {}, { kind, provider: 'pinecone', operation: 'upsertVectors', message });
}
function queryFailure(branch: string, kind: Failure['kind'], message: string): QueryAttempt<QueryVectorsOutput> {
  return newQueryBranch(branch, emptyQuery, {}, { kind, provider: 'pinecone', operation: 'queryVectors', message });
}
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
