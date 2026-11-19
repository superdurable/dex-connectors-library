// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetConversationStep, type Connection, type GetConversationInput, type GetConversationOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'meiqia';
const resultAttribute = new Attribute<GetConversationOutput>('meiqia-result', jsonCodec<GetConversationOutput>());

/** Query Meiqia through a typed connector Step. */
export class MeiqiaInspectionFlow implements Flow<GetConversationInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Meiqia connection name differs');
    this.read = newGetConversationStep<GetConversationInput>({
      stepType: 'CallGetConversation',
      annotations: { groupId: 'provider', groupLabel: 'Meiqia', explanation: 'Read provider data through Meiqia.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteMeiqia'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'MeiqiaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetConversationInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetConversationOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMeiqia'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetConversationOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
