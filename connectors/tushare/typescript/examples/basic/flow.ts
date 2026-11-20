// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTradingDayStep, type Connection, type GetTradingDayInput, type GetTradingDayOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tushare';
const resultAttribute = new Attribute<GetTradingDayOutput>('tushare-result', jsonCodec<GetTradingDayOutput>());

/** Query Tushare through a typed connector Step. */
export class TushareInspectionFlow implements Flow<GetTradingDayInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Tushare connection name differs');
    this.read = newGetTradingDayStep<GetTradingDayInput>({
      stepType: 'CallGetTradingDay',
      annotations: { groupId: 'provider', groupLabel: 'Tushare', explanation: 'Read provider data through Tushare.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteTushare'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TushareInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetTradingDayInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetTradingDayOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTushare'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetTradingDayOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
