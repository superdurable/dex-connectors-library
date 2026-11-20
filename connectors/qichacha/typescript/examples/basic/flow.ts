// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEnterpriseBasicDetailsStep, type Connection, type GetEnterpriseBasicDetailsInput, type GetEnterpriseBasicDetailsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qichacha';
const resultAttribute = new Attribute<GetEnterpriseBasicDetailsOutput>('qichacha-result', jsonCodec<GetEnterpriseBasicDetailsOutput>());

/** Query Qichacha through a typed connector Step. */
export class QichachaInspectionFlow implements Flow<GetEnterpriseBasicDetailsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qichacha connection name differs');
    this.read = newGetEnterpriseBasicDetailsStep<GetEnterpriseBasicDetailsInput>({
      stepType: 'CallGetEnterpriseBasicDetails',
      annotations: { groupId: 'provider', groupLabel: 'Qichacha', explanation: 'Read provider data through Qichacha.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteQichacha'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QichachaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetEnterpriseBasicDetailsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetEnterpriseBasicDetailsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQichacha'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetEnterpriseBasicDetailsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
