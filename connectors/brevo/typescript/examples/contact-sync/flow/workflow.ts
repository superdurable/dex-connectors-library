// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newCreateOrUpdateContactStep, newGetContactStep, type Connection, type Contact, type ContactWriteResult, type CreateOrUpdateContactInput } from '@superdurable/connector-brevo';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'brevo-contacts';

/** Safe contact write result retained across Steps. */
export const contactWrite = new Attribute<ContactWriteResult>('brevo-contact-write', jsonCodec<ContactWriteResult>());
/** Safe contact identity retained across Steps. */
export const contactIdentity = new Attribute<Contact>('brevo-contact-identity', jsonCodec<Contact>());

/** Create or update one Brevo contact, then verify its returned identity. */
export class ContactSyncFlow implements Flow<CreateOrUpdateContactInput> {
  private readonly prepare = new PrepareContactSync();
  private readonly afterWrite = new ContinueContactSync();
  private readonly complete = new CompleteContactSync();

  /** Bind a named Brevo connection before Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Brevo connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'BrevoContactSyncFlow'; }

  /** Register only the happy branches; optional outcomes fail the Flow. */
  getSteps(): StepList<CreateOrUpdateContactInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newCreateOrUpdateContactStep({
        stepType: 'SaveBrevoContact',
        annotations: { groupId: 'brevo-contact-sync', groupLabel: 'Brevo Contacts', explanation: 'Create or update a Brevo contact by email.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        saved: this.afterWrite,
      }),
      this.afterWrite,
      newGetContactStep({
        stepType: 'GetBrevoContact',
        annotations: { groupId: 'brevo-contact-sync', groupLabel: 'Brevo Contacts', explanation: 'Read back the saved contact identity.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }

  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [contactWrite, contactIdentity] }; }
}

/** Validate the input before sending it to the provider Step. */
// dex:group group-id:brevo-contact-sync group-label:"Brevo Contacts"
// dex:explanation text:"Check a contact email before the Brevo write Step."
export class PrepareContactSync implements Step<CreateOrUpdateContactInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareBrevoContactSync'; }
  /** Start the contact write with nonsecret input. */
  execute(_context: Context, input: CreateOrUpdateContactInput): StepDecision {
    if (!input.email?.includes('@')) return forceFail('A contact email is required');
    return goTo(new StepRef('SaveBrevoContact'), input);
  }
}

/** Save the write result and request its contact identity. */
// dex:group group-id:brevo-contact-sync group-label:"Brevo Contacts"
// dex:explanation text:"Retain the safe write result before reading the contact."
export class ContinueContactSync implements Step<MutationResult<ContactWriteResult>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'ContinueBrevoContactSync'; }
  /** Route the submitted email into the lookup Step. */
  execute(context: Context, result: MutationResult<ContactWriteResult>): StepDecision {
    contactWrite.set(context, result.value);
    return goTo(new StepRef('GetBrevoContact'), { email: result.value.email });
  }
}

/** Complete the Flow with Brevo's safe contact identity. */
// dex:group group-id:brevo-contact-sync group-label:"Brevo Contacts"
// dex:explanation text:"Persist the returned contact identity and complete sync."
export class CompleteContactSync implements Step<QueryResult<Contact>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteBrevoContactSync'; }
  /** Persist and return the contact identity. */
  execute(context: Context, result: QueryResult<Contact>): StepDecision {
    contactIdentity.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
