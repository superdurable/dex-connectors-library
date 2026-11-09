// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContactIDsStep, type Connection, type ListContactIDsInput, type ContactIDsPage } from '@superdurable/connector-nimble';

/** Static Dex Web connection for the Nimble example. */
export const ConnectionName = 'nimble';
const resultAttribute = new Attribute<ContactIDsPage>('nimble-contact-ids-page', jsonCodec<ContactIDsPage>());

/** List contact IDs visible to the connected Nimble user. */
export class ContactIDDiscoveryFlow implements Flow<ListContactIDsInput> {
  private readonly complete = new CompleteContactIDDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Nimble connection name differs');
    this.read = newListContactIDsStep<ListContactIDsInput>({
      stepType: 'CallListContactIDs',
      annotations: { groupId: 'provider', groupLabel: 'Nimble', explanation: 'List contact IDs visible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'NimbleContactIDDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListContactIDsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the contact ID page and complete the Flow."
class CompleteContactIDDiscovery implements Step<QueryResult<ContactIDsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<ContactIDsPage>>();
  getStepType(): string { return 'CompleteListContactIDs'; }
  execute(context: Context, result: QueryResult<ContactIDsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
