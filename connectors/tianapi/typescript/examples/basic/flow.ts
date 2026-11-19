// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetHolidayStatusStep, type Connection, type GetHolidayStatusInput, type GetHolidayStatusOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tianapi';
const resultAttribute = new Attribute<GetHolidayStatusOutput>('tianapi-result', jsonCodec<GetHolidayStatusOutput>());

/** Query TianAPI through a typed connector Step. */
export class TianAPIInspectionFlow implements Flow<GetHolidayStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TianAPI connection name differs');
    this.read = newGetHolidayStatusStep<GetHolidayStatusInput>({
      stepType: 'CallGetHolidayStatus',
      annotations: { groupId: 'provider', groupLabel: 'TianAPI', explanation: 'Read provider data through TianAPI.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteTianAPI'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TianAPIInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetHolidayStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetHolidayStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTianAPI'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetHolidayStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
