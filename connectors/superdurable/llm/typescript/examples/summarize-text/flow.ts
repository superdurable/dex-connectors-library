// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, Stream, gracefulComplete, jsonCodec, stringCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGenerateTextStep, type Connection, type GenerateTextRequest, type GenerateTextResponse } from '../../src/index.js';

/** Static Dex Web connection holding the provider choice, model, and secret key. */
export const ConnectionName = 'llm';
/** Persisted text deltas emitted by the provider response stream. */
export const generatedText = new Stream('llm-summarize-generated-text', stringCodec, 1 << 20);

/** Summarize one text generation request using the configured provider. */
export class SummarizeTextFlow implements Flow<GenerateTextRequest> {
  private readonly completed = new SummaryCompleted();
  private readonly generate;

  /** Bind the one static LLM connection before Worker registration. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('summarize-text requires the static llm connection');
    this.generate = newGenerateTextStep<GenerateTextRequest>({
      stepType: 'SummarizeTextGeneration',
      annotations: { groupId: 'llm-summary', groupLabel: 'LLM summary', explanation: 'Generate the requested summary with the connected model.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      generated: this.completed, textStream: generatedText,
    });
  }
  /** Stable Flow identity for Dex registration. */
  getFlowType(): string { return 'LLMSummarizeText'; }
  /** Register the generation Step and successful completion. */
  getSteps(): StepList<GenerateTextRequest> { return StepList.startStep(this.generate).otherSteps(this.completed); }
  /** Persist the provider's text deltas. */
  getPersistenceSchema() { return { streams: [generatedText] }; }
}

// dex:group group-id:llm-summary group-label:"LLM summary"
// dex:explanation text:"Complete the Flow with the generated text and token usage."
class SummaryCompleted implements Step<QueryResult<GenerateTextResponse>> {
  getStepType(): string { return 'SummarizeTextCompleted'; }
  execute(_context: Context, result: QueryResult<GenerateTextResponse>): StepDecision {
    return gracefulComplete(result.value);
  }
}

/** Input codec shared by the Worker and starter. */
export const requestCodec = jsonCodec<GenerateTextRequest>();
