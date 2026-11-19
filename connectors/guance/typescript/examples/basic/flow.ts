// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDashboardsPageStep, type Connection, type ListDashboardsPageInput, type ListDashboardsPageOutput } from '@superdurable/connector-guance';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'guance';
const resultAttribute = new Attribute<ListDashboardsPageOutput>('guance-result', jsonCodec<ListDashboardsPageOutput>());

/** Query Guance through a typed connector Step. */
export class GuanceInspectionFlow implements Flow<ListDashboardsPageInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Guance connection name differs');
    this.read = newListDashboardsPageStep<ListDashboardsPageInput>({
      stepType: 'CallListDashboardsPage',
      annotations: { groupId: 'provider', groupLabel: 'Guance', explanation: 'Read provider data through Guance.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteGuance'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GuanceInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListDashboardsPageInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListDashboardsPageOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGuance'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListDashboardsPageOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
