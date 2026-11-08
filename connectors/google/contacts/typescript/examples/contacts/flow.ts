// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContactsStep, type Connection, type ListContactsInput, type ContactsPage } from '@superdurable/connector-google-contacts';

/** Static Dex Web connection for the Contacts example. */
export const ConnectionName = 'google-contacts';
const resultAttribute = new Attribute<ContactsPage>('google-contacts-typescript-page', jsonCodec<ContactsPage>());

/** List contacts visible to the connected Google user. */
export class ContactsFlow implements Flow<ListContactsInput> {
  private readonly complete = new CompleteContacts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Contacts connection name differs');
    this.read = newListContactsStep<ListContactsInput>({
      stepType: 'CallListContacts',
      annotations: { groupId: 'provider', groupLabel: 'Google Contacts', explanation: 'List contacts available to the connected Google user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleContactsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListContactsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the contacts and complete the Flow."
class CompleteContacts implements Step<QueryResult<ContactsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<ContactsPage>>();
  getStepType(): string { return 'CompleteContacts'; }
  execute(context: Context, result: QueryResult<ContactsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
