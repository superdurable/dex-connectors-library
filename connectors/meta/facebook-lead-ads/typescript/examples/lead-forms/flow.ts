// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListLeadFormsStep, type Connection, type ListLeadFormsInput, type LeadFormsPage } from '@superdurable/connector-meta-facebook-lead-ads';

/** Static Dex Web connection for the Facebook Lead Ads example. */
export const ConnectionName = 'facebook-lead-ads';
const resultAttribute = new Attribute<LeadFormsPage>('facebook-lead-ads-forms-page', jsonCodec<LeadFormsPage>());

/** List forms visible to the connected Page token. */
export class LeadFormsFlow implements Flow<ListLeadFormsInput> {
  private readonly complete = new CompleteLeadForms();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Facebook Lead Ads connection name differs');
    this.read = newListLeadFormsStep<ListLeadFormsInput>({
      stepType: 'CallListLeadForms',
      annotations: { groupId: 'provider', groupLabel: 'Facebook Lead Ads', explanation: 'List Page lead-form IDs and names using the Page token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FacebookLeadForms'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListLeadFormsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the form page and complete the Flow."
class CompleteLeadForms implements Step<QueryResult<LeadFormsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<LeadFormsPage>>();
  getStepType(): string { return 'CompleteListLeadForms'; }
  execute(context: Context, result: QueryResult<LeadFormsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
