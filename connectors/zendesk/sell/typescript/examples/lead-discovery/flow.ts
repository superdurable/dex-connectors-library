// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListLeadsStep, type Connection, type ListLeadsInput, type LeadsPage } from '@superdurable/connector-zendesk-sell';

/** Static Dex Web connection for the Zendesk Sell example. */
export const ConnectionName = 'zendesk-sell';
const resultAttribute = new Attribute<LeadsPage>('zendesk-sell-leads-page', jsonCodec<LeadsPage>());

/** List leads visible to the connected Zendesk Sell user. */
export class LeadDiscoveryFlow implements Flow<ListLeadsInput> {
  private readonly complete = new CompleteLeadDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Zendesk Sell connection name differs');
    this.read = newListLeadsStep<ListLeadsInput>({
      stepType: 'CallListLeads',
      annotations: { groupId: 'provider', groupLabel: 'ZendeskSell', explanation: 'List leads visible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ZendeskSellLeadDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListLeadsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the lead page and complete the Flow."
class CompleteLeadDiscovery implements Step<QueryResult<LeadsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<LeadsPage>>();
  getStepType(): string { return 'CompleteListLeads'; }
  execute(context: Context, result: QueryResult<LeadsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
