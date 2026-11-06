// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContactsStep, type Connection, type ContactPage, type ListContactsInput } from '@superdurable/connector-xero';

/** Static Xero connection used by the Worker and starter. */
export const ConnectionName = 'xero-contacts';

/** Safe query retained by the Flow. */
export const contactRequest = new Attribute<ListContactsInput>('xero-list-contacts-request', jsonCodec<ListContactsInput>());
/** Contact page retained by the Flow. */
export const contactPage = new Attribute<ContactPage>('xero-list-contacts-page', jsonCodec<ContactPage>());

/** List one bounded Xero contact page. */
export class ListContactsFlow implements Flow<ListContactsInput> {
  private readonly complete = new CompleteListContacts();
  private readonly listContacts;
  private readonly prepare;

  /** Bind the named Xero connection at Worker registration. */
  constructor(connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('Xero contacts connection is required');
    this.listContacts = newListContactsStep<ListContactsInput>({ stepType: 'ListXeroContacts', annotations: { groupId: 'xero-contacts', groupLabel: 'Xero Contacts', explanation: 'List one bounded page of Xero contacts.' }, connection, connectionName: ConnectionName, mapToOperationInput: input => input, listed: this.complete });
    this.prepare = new PrepareListContacts(this.listContacts);
  }

  /** Stable Flow identity. */ getFlowType(): string { return 'XeroListContactsFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<ListContactsInput> {
    return StepList.startStep(this.prepare).otherSteps(this.listContacts, this.complete);
  }
  /** Register the persisted request and page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [contactRequest, contactPage] }; }
}

// dex:group group-id:xero-contacts group-label:"Xero Contacts"
// dex:explanation text:"Validate the bounded page request before listing Xero contacts."
class PrepareListContacts implements Step<ListContactsInput> {
  constructor(private readonly listContacts: Step<ListContactsInput>) {}
  getStepType(): string { return 'PrepareXeroListContacts'; }
  execute(context: Context, input: ListContactsInput): StepDecision {
    if (input.pageSize !== undefined && (input.pageSize < 1 || input.pageSize > 100)) return forceFail('pageSize must be between 1 and 100');
    contactRequest.set(context, input);
    return goTo(this.listContacts, input);
  }
}

// dex:group group-id:xero-contacts group-label:"Xero Contacts"
// dex:explanation text:"Persist the contact page and complete the Flow."
class CompleteListContacts implements Step<QueryResult<ContactPage>> {
  getStepType(): string { return 'CompleteXeroListContacts'; }
  execute(context: Context, result: QueryResult<ContactPage>): StepDecision {
    contactPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
