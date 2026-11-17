// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListKnowledgeBasesStep, type Connection, type ListKnowledgeBasesInput, type ListKnowledgeBasesOutput } from '@superdurable/connector-ctyun';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'ctyun';
/** Persisted knowledge-base resource directory. */ export const knowledgeBasesAttribute = new Attribute<ListKnowledgeBasesOutput>('ctyun-knowledge-bases', jsonCodec<ListKnowledgeBasesOutput>());

/** List accessible CTYun knowledge-base resources. */
export class CTYunKnowledgeBasesFlow implements Flow<ListKnowledgeBasesInput> {
  private readonly complete = new CompleteKnowledgeBases();
  private readonly read;
  /** Bind one static CTYun connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('CTYun connection name differs');
    this.read = newListKnowledgeBasesStep<ListKnowledgeBasesInput>({ stepType: 'CallListKnowledgeBases',
      annotations: { groupId: 'provider', groupLabel: 'CTYun', explanation: 'List knowledge-base IDs, names, and file counts.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: new StepRef('CompleteCTYunKnowledgeBases'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'CTYunKnowledgeBasesTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListKnowledgeBasesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted resource directory. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [knowledgeBasesAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the resource directory and complete the Flow."
class CompleteKnowledgeBases implements Step<QueryResult<ListKnowledgeBasesOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCTYunKnowledgeBases'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<ListKnowledgeBasesOutput>): StepDecision { knowledgeBasesAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
