// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/mock.js';

import { newMutation, newQuery, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import { extractStructuredAnswer, validatePortableRequest } from '@superdurable/connectors-sdk/textgen';
import type { Failure } from '@superdurable/connectors-sdk';
import { newConnectionWithOperations, type Connection, type Operations } from './generated/connection.js';
import { CreateResponseDefinition, RetrieveResponseDefinition } from './generated/definitions.js';
import type { CreateRequest, Response, RetrieveRequest } from './generated/models.js';
import { createResponseDefault, retrieveResponseDefault } from './generated/mock.js';
import { isPrintableModelID } from './client.js';

/** Check the request rules used by the real create operation. */
export function validateCreateResponseRequest(request: CreateRequest): Failure | undefined {
  const model = request.Model?.trim() || 'gpt-6-sol';
  if (request.Input === null || request.Input === undefined || !isPrintableModelID(model)) return { kind: 'VALIDATION', provider: 'openai', operation: 'createResponse', message: 'input and a model of printable ASCII without spaces are required' };
  try {
    if (!isJSONSerializable(request.Input) || (request.StructuredOutput && !isJSONSerializable(request.StructuredOutput))) throw new TypeError('unsupported JSON value');
    JSON.stringify(request.Input);
    if (request.StructuredOutput) JSON.stringify(request.StructuredOutput);
  } catch { return { kind: 'VALIDATION', provider: 'openai', operation: 'createResponse', message: 'request is not JSON serializable' }; }
  return undefined;
}

/** Check a scripted completed answer using the SDK portable schema checker. */
export function validateCreateResponseAnswer(request: CreateRequest, answer: Response): void {
  if (answer.status && answer.status !== 'completed') throw new TypeError('a completed answer must have status completed or none');
  const output = request.StructuredOutput;
  if (!output || !answer.outputText) return;
  try { JSON.parse(answer.outputText); }
  catch { throw new TypeError("the request sets StructuredOutput, so a completed answer's outputText must be exactly one JSON value"); }
  if (!output.strict || !output.schema) return;
  const portable = { messages: [{ role: 'user' as const, text: 'check' }], structuredOutput: { name: output.name, description: output.description ?? '', schema: output.schema } };
  if (validatePortableRequest(portable)) return;
  try { extractStructuredAnswer(portable, answer.outputText); }
  catch { throw new TypeError("the request sets StructuredOutput with Strict, so a completed answer's outputText must match its schema"); }
}

function isJSONSerializable(value: unknown, seen = new Set<object>()): boolean {
  if (typeof value === 'function' || typeof value === 'symbol' || typeof value === 'bigint' || typeof value === 'undefined') return false;
  if (typeof value === 'number' && !Number.isFinite(value)) return false;
  if (!value || typeof value !== 'object') return true;
  if (seen.has(value)) return false;
  seen.add(value);
  const valid = Object.values(value).every(child => isJSONSerializable(child, seen));
  seen.delete(value);
  return valid;
}

/** Scripted OpenAI operations with Go-equivalent create request and answer checks. */
export class Mock implements Operations {
  private readonly create: MockMutation<CreateRequest, Response>;
  private readonly retrieve: MockQuery<RetrieveRequest, Response>;
  private readonly bound: Connection;
  /** Bind one static connection name. */
  constructor(connectionName: string) {
    this.create = newMutation<CreateRequest, Response>(CreateResponseDefinition, { requestCheck: validateCreateResponseRequest, answerCheck: { branch: 'completed', check: validateCreateResponseAnswer } }).default(createResponseDefault());
    this.retrieve = newQuery<RetrieveRequest, Response>(RetrieveResponseDefinition).default(retrieveResponseDefault());
    this.bound = newConnectionWithOperations(this, { provider: 'openai', name: connectionName });
  }
  /** Return the bound connection. */ connection(): Connection { return this.bound; }
  /** Script the create Mutation. */ createResponse(): MockMutation<CreateRequest, Response> { return this.create; }
  /** Script the retrieve Query. */ retrieveResponse(): MockQuery<RetrieveRequest, Response> { return this.retrieve; }
  /** Assert that every scripted call was consumed without a defect. */
  assertNoFailures(): void { this.create.assertNoFailures(); this.retrieve.assertNoFailures(); }
}
