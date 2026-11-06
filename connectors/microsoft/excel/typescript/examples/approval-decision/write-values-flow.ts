// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newUpdateValuesStep, type UpdateValuesInput, type UpdateValuesOutput, type Connection } from '@superdurable/connector-microsoft-excel';

/** Static name of the Dex Web Microsoft Excel connection. */
export const ConnectionName = 'approvals';

/** Write literal values to one worksheet range. */
export class WriteValuesFlow implements Flow<UpdateValuesInput> {
  private readonly complete = new CompleteValuesWrite();
  constructor(private readonly connection: Connection) {}
  getFlowType(): string { return 'MicrosoftExcelTypeScriptWriteValues'; }
  getSteps(): StepList<UpdateValuesInput> { return StepList.startStep(newUpdateValuesStep<UpdateValuesInput>({
    stepType: 'WriteExcelValues', annotations: { groupId: 'excel', groupLabel: 'Excel', explanation: 'Write literal worksheet cells.' },
    connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, updated: this.complete,
  })).otherSteps(this.complete); }
}

/** Complete the absolute worksheet write. */
// dex:group group-id:excel group-label:"Excel"
// dex:explanation text:"Return the range that was written."
export class CompleteValuesWrite implements Step<MutationResult<UpdateValuesOutput>> {
  getStepType(): string { return 'CompleteExcelValuesWrite'; }
  execute(_context: Context, result: MutationResult<UpdateValuesOutput>): StepDecision { return gracefulComplete(result); }
}
