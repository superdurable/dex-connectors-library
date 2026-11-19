// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCreateResponseStep, type Connection, type CreateRequest, type Response } from '../../src/index.js';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'openai-api';
/** Safe summary request. */
export interface SummaryRequest { text: string }
/** Stored response identity, summary, and usage. */
export interface SummaryOutcome { responseId: string; summary: string; status: string; model: string; usage: Response['usage'] }

const summaryRequest = new Attribute('openai-summary-request-ts', jsonCodec<SummaryRequest>());
const summaryOutcome = new Attribute('openai-summary-outcome-ts', jsonCodec<SummaryOutcome>());

/** Create one stored OpenAI response from a durable summary request. */
export class SummarizeTextFlow implements Flow<SummaryRequest> {
  private readonly completed = new CompleteSummary();
  private readonly create;
  private readonly start;
  /** Bind one named OpenAI connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('summarize text requires its static OpenAI connection');
    this.create = newCreateResponseStep<SummaryRequest>({
      stepType: 'CreateOpenAISummary',
      annotations: { groupId: 'summary', groupLabel: 'Summary', explanation: 'Store an OpenAI Response that summarizes the submitted text.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: mapToCreateRequest,
      completed: this.completed,
    });
    this.start = new StartSummary(this.create);
  }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'OpenAISummarizeTextTS'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<SummaryRequest> { return StepList.startStep<SummaryRequest>(this.start).otherSteps(this.create, this.completed); }
  /** Register every durable Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [summaryRequest, summaryOutcome] }; }
}

/** Map safe application text to the generated Go-compatible request. */
export function mapToCreateRequest(request: SummaryRequest): CreateRequest {
  return { Model: '', Input: request.text, Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.", StructuredOutput: null };
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Validate and persist the text before calling OpenAI."
class StartSummary implements Step<SummaryRequest> {
  readonly inputCodec = jsonCodec<SummaryRequest>();
  constructor(private readonly create: Step<SummaryRequest>) {}
  getStepType(): string { return 'StartOpenAISummaryTS'; }
  execute(context: Context, request: SummaryRequest): StepDecision {
    if (typeof request.text !== 'string' || !request.text.trim() || Buffer.byteLength(request.text) > 64 * 1024) return forceFail('summary request requires non-empty text of at most 64 KiB');
    context.setAttribute(summaryRequest, request);
    return goTo(this.create, request);
  }
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Persist the stored response identity and summary."
class CompleteSummary implements Step<MutationResult<Response>> {
  getStepType(): string { return 'CompleteOpenAISummaryTS'; }
  execute(context: Context, result: MutationResult<Response>): StepDecision {
    const outcome: SummaryOutcome = { responseId: result.value.id, summary: result.value.outputText, status: result.value.status, model: result.value.model, usage: result.value.usage };
    context.setAttribute(summaryOutcome, outcome);
    return gracefulComplete(outcome);
  }
}
