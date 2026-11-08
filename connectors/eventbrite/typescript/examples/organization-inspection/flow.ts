// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListOrganizationsStep, type Connection, type ListOrganizationsInput, type OrganizationsPage } from '@superdurable/connector-eventbrite';

/** Static Dex Web connection for the Eventbrite example. */
export const ConnectionName = 'eventbrite';
const resultAttribute = new Attribute<OrganizationsPage>('eventbrite-organizations-page', jsonCodec<OrganizationsPage>());

/** List organizations visible to the connected Eventbrite account. */
export class OrganizationInspectionFlow implements Flow<ListOrganizationsInput> {
  private readonly complete = new CompleteOrganizationInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Eventbrite connection name differs');
    this.read = newListOrganizationsStep<ListOrganizationsInput>({
      stepType: 'CallListOrganizations',
      annotations: { groupId: 'provider', groupLabel: 'Eventbrite', explanation: 'List organizations accessible to the connected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'EventbriteOrganizationInspection'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListOrganizationsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the organization page and complete the Flow."
class CompleteOrganizationInspection implements Step<QueryResult<OrganizationsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<OrganizationsPage>>();
  getStepType(): string { return 'CompleteListOrganizations'; }
  execute(context: Context, result: QueryResult<OrganizationsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
