// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContactsStep, type Connection, type ListContactsInput, type ContactsPage } from '@superdurable/connector-activecampaign';

/** Static Dex Web connection for the ActiveCampaign example. */
export const ConnectionName = 'activecampaign';
const resultAttribute = new Attribute<ContactsPage>('activecampaign-contacts-page', jsonCodec<ContactsPage>());

/** List contacts visible to the connected ActiveCampaign account. */
export class ContactInspectionFlow implements Flow<ListContactsInput> {
  private readonly complete = new CompleteContactInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ActiveCampaign connection name differs');
    this.read = newListContactsStep<ListContactsInput>({
      stepType: 'CallListContacts',
      annotations: { groupId: 'provider', groupLabel: 'ActiveCampaign', explanation: 'List contacts accessible to the connected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ActiveCampaignContactInspection'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListContactsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the contact page and complete the Flow."
class CompleteContactInspection implements Step<QueryResult<ContactsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<ContactsPage>>();
  getStepType(): string { return 'CompleteListContacts'; }
  execute(context: Context, result: QueryResult<ContactsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
