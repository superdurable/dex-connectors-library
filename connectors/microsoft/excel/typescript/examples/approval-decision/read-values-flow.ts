// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetValuesStep, type GetValuesInput, type GetValuesOutput, type Connection } from '@superdurable/connector-microsoft-excel';

/** Static name of the Dex Web Microsoft Excel connection. */
export const ConnectionName = 'approvals';

/** Read one bounded worksheet range. */
export class ReadValuesFlow implements Flow<GetValuesInput> {
  private readonly complete = new CompleteValuesRead();
  constructor(private readonly connection: Connection) {}
  getFlowType(): string { return 'MicrosoftExcelTypeScriptReadValues'; }
  getSteps(): StepList<GetValuesInput> { return StepList.startStep(newGetValuesStep<GetValuesInput>({
    stepType: 'ReadExcelValues', annotations: { groupId: 'excel', groupLabel: 'Excel', explanation: 'Read raw and displayed worksheet cells.' },
    connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, read: this.complete,
  })).otherSteps(this.complete); }
}

/** Complete the worksheet read with its safe result. */
// dex:group group-id:excel group-label:"Excel"
// dex:explanation text:"Return the raw and displayed worksheet values."
export class CompleteValuesRead implements Step<QueryResult<GetValuesOutput>> {
  getStepType(): string { return 'CompleteExcelValuesRead'; }
  execute(_context: Context, result: QueryResult<GetValuesOutput>): StepDecision { return gracefulComplete(result); }
}
