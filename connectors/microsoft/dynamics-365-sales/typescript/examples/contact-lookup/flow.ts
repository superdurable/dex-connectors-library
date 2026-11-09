// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newFindContactStep, type Connection, type Contact, type FindContactInput,
} from '@superdurable/connector-microsoft-dynamics-365-sales';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'microsoft-dynamics-365-sales';
const functionDetailsAttribute = new Attribute<Contact>('dynamics365sales-contact-lookup', jsonCodec<Contact>());

/** Find one contact through a typed Microsoft Dynamics 365 Sales Query. */
export class ContactFlow implements Flow<FindContactInput> {
  private readonly complete = new CompleteContact();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Microsoft Dynamics 365 Sales connection name differs');
    this.read = newFindContactStep<FindContactInput>({
      stepType: 'CallFindContact',
      annotations: { groupId: 'provider', groupLabel: 'Microsoft Dynamics 365 Sales', explanation: 'Find one contact by email in Dynamics 365 Sales.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteFindContact'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MicrosoftDynamics365SalesContactLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<FindContactInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected contact. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the found contact and complete the Flow."
class CompleteContact implements Step<QueryResult<Contact>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFindContact'; }
  /** Persist and return the found contact. */
  execute(context: Context, result: QueryResult<Contact>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
