// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTableRowsStep, type GetTableRowsInput, type GetTableRowsOutput, type Connection } from '@superdurable/connector-microsoft-excel';

/** Static name of the Dex Web Microsoft Excel connection. */
export const ConnectionName = 'approvals';

/** Read table rows using a named Microsoft Excel connection. */
export class ReadTableFlow implements Flow<GetTableRowsInput> {
  private readonly complete = new CompleteTableRead();
  constructor(private readonly connection: Connection) {}
  getFlowType(): string { return 'MicrosoftExcelTypeScriptReadTable'; }
  getSteps(): StepList<GetTableRowsInput> { return StepList.startStep(newGetTableRowsStep<GetTableRowsInput>({
    stepType: 'ReadExcelTable', annotations: { groupId: 'excel', groupLabel: 'Excel', explanation: 'Read an Excel table as keyed rows.' },
    connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, read: this.complete,
  })).otherSteps(this.complete); }
}

/** Complete the table read with its safe result. */
// dex:group group-id:excel group-label:"Excel"
// dex:explanation text:"Return the keyed table rows after the read completes."
export class CompleteTableRead implements Step<QueryResult<GetTableRowsOutput>> {
  getStepType(): string { return 'CompleteExcelTableRead'; }
  execute(_context: Context, result: QueryResult<GetTableRowsOutput>): StepDecision { return gracefulComplete(result); }
}
