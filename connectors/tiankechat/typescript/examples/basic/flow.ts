// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListKnowledgeEntriesStep, type Connection, type ListKnowledgeEntriesInput, type ListKnowledgeEntriesOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tiankechat';
const resultAttribute = new Attribute<ListKnowledgeEntriesOutput>('tiankechat-result', jsonCodec<ListKnowledgeEntriesOutput>());

/** Query TiankeChat through a typed connector Step. */
export class TiankeChatInspectionFlow implements Flow<ListKnowledgeEntriesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TiankeChat connection name differs');
    this.read = newListKnowledgeEntriesStep<ListKnowledgeEntriesInput>({
      stepType: 'CallListKnowledgeEntries',
      annotations: { groupId: 'provider', groupLabel: 'TiankeChat', explanation: 'Read provider data through TiankeChat.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteTiankeChat'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TiankeChatInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListKnowledgeEntriesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListKnowledgeEntriesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTiankeChat'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListKnowledgeEntriesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
