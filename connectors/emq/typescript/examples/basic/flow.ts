// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDeploymentsStep, type Connection, type ListDeploymentsInput, type ListDeploymentsOutput } from '@superdurable/connector-emq';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'emq';
const resultAttribute = new Attribute<ListDeploymentsOutput>('emq-result', jsonCodec<ListDeploymentsOutput>());

/** Query Emq through a typed connector Step. */
export class EmqInspectionFlow implements Flow<ListDeploymentsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Emq connection name differs');
    this.read = newListDeploymentsStep<ListDeploymentsInput>({
      stepType: 'CallListDeployments',
      annotations: { groupId: 'provider', groupLabel: 'Emq', explanation: 'Read provider data through Emq.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteEmq'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'EmqInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListDeploymentsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListDeploymentsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteEmq'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListDeploymentsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
