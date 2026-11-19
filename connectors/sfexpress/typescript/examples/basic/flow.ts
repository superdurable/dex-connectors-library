// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryRoutesStep, type Connection, type QueryRoutesInput, type RouteQueryResult } from '@superdurable/connector-sfexpress';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'sfexpress';
const resultAttribute = new Attribute<RouteQueryResult>('sfexpress-result', jsonCodec<RouteQueryResult>());

/** Query SFExpress through a typed connector Step. */
export class SFExpressInspectionFlow implements Flow<QueryRoutesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SFExpress connection name differs');
    this.read = newQueryRoutesStep<QueryRoutesInput>({
      stepType: 'CallQueryRoutes',
      annotations: { groupId: 'provider', groupLabel: 'SFExpress', explanation: 'Read provider data through SFExpress.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteSFExpress'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SFExpressInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<QueryRoutesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<RouteQueryResult>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSFExpress'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<RouteQueryResult>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
