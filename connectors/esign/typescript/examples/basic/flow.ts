// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetSignFlowStatusStep, type Connection, type GetSignFlowStatusInput, type GetSignFlowStatusOutput } from '@superdurable/connector-esign';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'esign';
const resultAttribute = new Attribute<GetSignFlowStatusOutput>('esign-result', jsonCodec<GetSignFlowStatusOutput>());

/** Query Esign through a typed connector Step. */
export class EsignInspectionFlow implements Flow<GetSignFlowStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Esign connection name differs');
    this.read = newGetSignFlowStatusStep<GetSignFlowStatusInput>({
      stepType: 'CallGetSignFlowStatus',
      annotations: { groupId: 'provider', groupLabel: 'Esign', explanation: 'Read provider data through Esign.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteEsign'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'EsignInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetSignFlowStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetSignFlowStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteEsign'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetSignFlowStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
