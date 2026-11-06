// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetRecordStep, type Connection, type GetRecordInput, type Record as SalesforceRecord } from '@superdurable/connector-salesforce';

/** Static Dex Web connection name for this runnable example. */
export const ConnectionName = 'salesforce-crm';

/** Runnable Flow that reads one Salesforce record through a named connection. */
export class SalesforceRecordLookupFlow implements Flow<GetRecordInput> {
  private readonly completed = new RecordReadStep();
  private readonly read;

  /** Bind the Salesforce connection before registering the operation Step. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('record lookup requires salesforce-crm');
    this.read = newGetRecordStep<GetRecordInput>({
      stepType: 'ReadSalesforceRecord', connectionName: ConnectionName, connection,
      annotations: {
        groupId: 'salesforce-lookup', groupLabel: 'Salesforce record lookup',
        explanation: 'Read the selected Salesforce record.',
      },
      mapToOperationInput: input => input, found: this.completed,
    });
  }

  /** Stable Flow identity. */
  getFlowType(): string { return 'SalesforceRecordLookup'; }
  /** Register the connector Step and completion target. */
  getSteps() { return StepList.startStep(this.read).otherSteps(this.completed); }
}

// dex:group group-id:salesforce-lookup group-label:"Salesforce record lookup"
// dex:explanation text:"Complete after Salesforce returns the selected record."
class RecordReadStep implements Step<QueryResult<SalesforceRecord>> {
  /** Stable completion Step identity. */
  getStepType(): string { return 'CompleteSalesforceRecordRead'; }
  /** Complete with the non-secret connector Result. */
  execute(context: Context, result: QueryResult<SalesforceRecord>): StepDecision {
    void context;
    return forceComplete(result);
  }
}
