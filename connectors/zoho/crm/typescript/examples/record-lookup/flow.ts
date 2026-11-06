// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetRecordStep, type Connection, type GetRecordInput, type Record as ZohoRecord } from '@superdurable/connector-zoho-crm';

/** Static name of the Zoho CRM connection configured in Dex Web. */
export const ConnectionName = 'zoho-crm-main';

/** A runnable Flow that reads one Zoho CRM record. */
export class ZohoRecordLookupFlow implements Flow<GetRecordInput> {
  private readonly completedStep = new LookupCompletedStep();
  private readonly readStep;

  /** Bind a statically named Zoho CRM connection. */
  constructor(connection: Connection) {
    this.readStep = newGetRecordStep<GetRecordInput>({
      stepType: 'ReadZohoCRMRecord', connectionName: ConnectionName, connection,
      annotations: { groupId: 'zoho-crm', groupLabel: 'Zoho CRM', explanation: 'Read the requested CRM record.' },
      mapToOperationInput: input => input, found: this.completedStep,
    });
  }

  /** Stable Flow identity registered by a Dex Worker. */
  getFlowType(): string { return 'ZohoCRMRecordLookupExample'; }
  /** Register the provider Step and its result receiver. */
  getSteps() { return StepList.startStep(this.readStep).otherSteps(this.completedStep); }
}

// dex:group group-id:zoho-crm group-label:"Zoho CRM"
// dex:explanation text:"Complete after the Zoho CRM record is read."
class LookupCompletedStep implements Step<QueryResult<ZohoRecord>> {
  getStepType(): string { return 'CompleteZohoCRMRecordLookup'; }
  execute(context: Context, result: QueryResult<ZohoRecord>): StepDecision { void context; return forceComplete(result); }
}
