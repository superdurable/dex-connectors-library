// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListContactViewsStep, type Connection, type ListContactViewsInput, type ContactViews } from '@superdurable/connector-freshworks-freshsales';

/** Static Dex Web connection for the Freshsales example. */
export const ConnectionName = 'freshsales';
const resultAttribute = new Attribute<ContactViews>('freshsales-contact-views', jsonCodec<ContactViews>());

/** List contacts visible to the connected Freshsales user. */
export class ContactViewDiscoveryFlow implements Flow<ListContactViewsInput> {
  private readonly complete = new CompleteContactViewDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Freshsales connection name differs');
    this.read = newListContactViewsStep<ListContactViewsInput>({
      stepType: 'CallListContactViews',
      annotations: { groupId: 'provider', groupLabel: 'Freshsales', explanation: 'List contacts visible to the connected user.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FreshsalesContactViewDiscovery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListContactViewsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the contact views and complete the Flow."
class CompleteContactViewDiscovery implements Step<QueryResult<ContactViews>> {
  readonly inputCodec = jsonCodec<QueryResult<ContactViews>>();
  getStepType(): string { return 'CompleteListContactViews'; }
  execute(context: Context, result: QueryResult<ContactViews>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
