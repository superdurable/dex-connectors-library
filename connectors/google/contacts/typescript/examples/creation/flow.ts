// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCreateContactStep, type Connection, type CreateContactInput, type Contact } from '@superdurable/connector-google-contacts';

/** Static Dex Web connection for the creation example. */
export const ConnectionName = 'google-contacts';
const resultAttribute = new Attribute<Contact>('google-created-contact-typescript', jsonCodec<Contact>());

/** Create one contact for the connected Google user. */
export class CreationFlow implements Flow<CreateContactInput> {
  private readonly complete = new CompleteCreation();
  private readonly create;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Contacts connection name differs');
    this.create = newCreateContactStep<CreateContactInput>({
      stepType: 'CallCreateContact',
      annotations: { groupId: 'provider', groupLabel: 'Google Contacts', explanation: 'Create one contact for the connected Google user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, created: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleContactsCreationTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<CreateContactInput> {
    return StepList.startStep(this.create).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created contact and complete the Flow."
class CompleteCreation implements Step<MutationResult<Contact>> {
  readonly inputCodec = jsonCodec<MutationResult<Contact>>();
  getStepType(): string { return 'CompleteCreation'; }
  execute(context: Context, result: MutationResult<Contact>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
