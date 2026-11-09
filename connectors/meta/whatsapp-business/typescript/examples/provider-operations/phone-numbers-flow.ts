// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPhoneNumbersStep, type Connection, type ListPhoneNumbersInput, type PhoneNumbersPage } from '@superdurable/connector-meta-whatsapp-business';

/** Static Dex Web connection for the WhatsApp Business example. */
export const ConnectionName = 'whatsapp-business';
const resultAttribute = new Attribute<PhoneNumbersPage>('whatsapp-business-phone-numbers', jsonCodec<PhoneNumbersPage>());

/** List business phone numbers visible to the connected system-user token. */
export class PhoneNumbersFlow implements Flow<ListPhoneNumbersInput> {
  private readonly complete = new CompletePagePosts();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WhatsApp Business connection name differs');
    this.read = newListPhoneNumbersStep<ListPhoneNumbersInput>({
      stepType: 'CallListPhoneNumbers',
      annotations: { groupId: 'provider', groupLabel: 'WhatsApp Business', explanation: 'List business phone-number identities using the system-user token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WhatsAppBusinessPhoneNumbers'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListPhoneNumbersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the post page and complete the Flow."
class CompletePagePosts implements Step<QueryResult<PhoneNumbersPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PhoneNumbersPage>>();
  getStepType(): string { return 'CompleteListPhoneNumbers'; }
  execute(context: Context, result: QueryResult<PhoneNumbersPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
