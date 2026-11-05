// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newAppendTableRowsStep, type AppendTableRowsInput, type AppendTableRowsOutput, type Connection } from '@superdurable/connector-microsoft-excel';

/** Static name of the Dex Web Microsoft Excel connection. */
export const ConnectionName = 'approvals';

/** Append only table rows whose key is absent. */
export class AppendRowsFlow implements Flow<AppendTableRowsInput> {
  private readonly complete = new CompleteRowsAppend();
  constructor(private readonly connection: Connection) {}
  getFlowType(): string { return 'MicrosoftExcelTypeScriptAppendRows'; }
  getSteps(): StepList<AppendTableRowsInput> { return StepList.startStep(newAppendTableRowsStep<AppendTableRowsInput>({
    stepType: 'AppendExcelRows', annotations: { groupId: 'excel', groupLabel: 'Excel', explanation: 'Append only absent keyed table rows.' },
    connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, appended: this.complete,
  })).otherSteps(this.complete); }
}

/** Complete the keyed table append. */
// dex:group group-id:excel group-label:"Excel"
// dex:explanation text:"Return which keys were appended or already present."
export class CompleteRowsAppend implements Step<MutationResult<AppendTableRowsOutput>> {
  getStepType(): string { return 'CompleteExcelRowsAppend'; }
  execute(_context: Context, result: MutationResult<AppendTableRowsOutput>): StepDecision { return gracefulComplete(result); }
}
