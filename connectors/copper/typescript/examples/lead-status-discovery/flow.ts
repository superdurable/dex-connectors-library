// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListLeadStatusesStep, type Connection, type ListLeadStatusesInput, type LeadStatusesList } from '@superdurable/connector-copper';

/** Static Dex Web connection for the Copper example. */
export const ConnectionName = 'copper';
const resultAttribute = new Attribute<LeadStatusesList>('copper-lead-statuses', jsonCodec<LeadStatusesList>());

/** List lead statuses visible to the connected Copper user. */
export class LeadStatusDiscoveryFlow implements Flow<ListLeadStatusesInput> {
  private readonly complete = new CompleteLeadStatusDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Copper connection name differs');
    this.read = newListLeadStatusesStep<ListLeadStatusesInput>({
      stepType: 'CallListLeadStatuses',
      annotations: { groupId: 'provider', groupLabel: 'Copper', explanation: 'List lead statuses visible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'CopperLeadStatusDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListLeadStatusesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist lead statuses and complete the Flow."
class CompleteLeadStatusDiscovery implements Step<QueryResult<LeadStatusesList>> {
  readonly inputCodec = jsonCodec<QueryResult<LeadStatusesList>>();
  getStepType(): string { return 'CompleteListLeadStatuses'; }
  execute(context: Context, result: QueryResult<LeadStatusesList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
