// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, jsonCodec, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGenerateContentStep, type Connection, type GenerateContentRequest, type GenerateContentResponse } from '@superdurable/connector-google-gemini';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'gemini-summary';
/** Safe generation result retained for inspection. */
export const generatedContent = new Attribute<GenerateContentResponse>('gemini-generated-content', jsonCodec<GenerateContentResponse>());

/** Summarize text with one native Gemini Step. */
export class GenerateSummaryFlow implements Flow<string> {
  private readonly complete = new CompleteSummary();
  /** Bind the Gemini connection when registering the Worker. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Gemini connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'GeminiGenerateSummaryFlow'; }
  /** Generate and complete on the happy path. */
  getSteps(): StepList<string> {
    return StepList.startStep(
      // dex:group group-id:gemini-summary group-label:"Gemini Summary"
      // dex:explanation text:"Ask Gemini for a summary of the submitted text."
      newGenerateContentStep({
      stepType: 'GenerateGeminiSummary',
      annotations: { groupId: 'gemini-summary', groupLabel: 'Gemini Summary', explanation: 'Ask Gemini to summarize the submitted text.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: (text: string): GenerateContentRequest => ({ contents: [{ role: 'user', parts: [{ text }] }], maxOutputTokens: 128 }),
      generated: this.complete,
      }),
    ).otherSteps(this.complete);
  }
  /** Register the persisted result Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [generatedContent] }; }
}

/** Persist the answer and complete the Flow. */
// dex:group group-id:gemini-summary group-label:"Gemini Summary"
// dex:explanation text:"Persist the generated Gemini answer and complete the summary Flow."
export class CompleteSummary implements Step<QueryResult<GenerateContentResponse>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGeminiSummary'; }
  /** Retain the safe result. */
  execute(context: Context, result: QueryResult<GenerateContentResponse>): StepDecision {
    generatedContent.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
