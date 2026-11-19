// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListKnowledgeBasesStep, type Connection, type ListKnowledgeBasesInput, type ListKnowledgeBasesOutput } from '../../src/index.js';

/** Static Dex Web connection for the Baichuan example. */
export const ConnectionName = 'baichuan';
const knowledgeBaseResult = new Attribute<ListKnowledgeBasesOutput>('baichuan-knowledge-bases-result', jsonCodec<ListKnowledgeBasesOutput>());

/** Read the knowledge bases visible to a Baichuan API key. */
export class KnowledgeBaseListFlow implements Flow<ListKnowledgeBasesInput> {
  private readonly start = new StartKnowledgeBaseList();
  private readonly complete = new CompleteKnowledgeBaseList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Baichuan connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'BaichuanKnowledgeBaseList'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListKnowledgeBasesInput> {
    return StepList.startStep(this.start).otherSteps(newListKnowledgeBasesStep({
      stepType: 'CallBaichuanKnowledgeBases',
      annotations: { groupId: 'baichuan', groupLabel: 'Baichuan AI', explanation: 'Read the knowledge bases visible to this key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteBaichuanKnowledgeBases'),
    }), this.complete);
  }
  /** Register the durable knowledge-base list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [knowledgeBaseResult] }; }
}

// dex:group group-id:baichuan group-label:"Baichuan AI"
// dex:explanation text:"Start the knowledge-base query."
class StartKnowledgeBaseList implements Step<ListKnowledgeBasesInput> {
  getStepType(): string { return 'StartBaichuanKnowledgeBases'; }
  execute(_context: Context, input: ListKnowledgeBasesInput): StepDecision { return goTo(new StepRef('CallBaichuanKnowledgeBases'), input); }
}

// dex:group group-id:baichuan group-label:"Baichuan AI"
// dex:explanation text:"Persist the knowledge-base list and complete the Flow."
class CompleteKnowledgeBaseList implements Step<QueryResult<ListKnowledgeBasesOutput>> {
  getStepType(): string { return 'CompleteBaichuanKnowledgeBases'; }
  execute(context: Context, result: QueryResult<ListKnowledgeBasesOutput>): StepDecision { knowledgeBaseResult.set(context, result.value); return gracefulComplete(result.value); }
}
