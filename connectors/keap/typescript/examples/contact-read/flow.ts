// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetContactStep, type Connection, type ContactProfile, type GetContactInput,
} from '@superdurable/connector-keap';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'keap';
const contactAttribute = new Attribute<ContactProfile>('keap-contact-read', jsonCodec<ContactProfile>());

/** Read contact profile through a typed Keap Query. */
export class ContactReadFlow implements Flow<GetContactInput> {
  private readonly complete = new CompleteContactRead();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Keap connection name differs');
    this.read = newGetContactStep<GetContactInput>({
      stepType: 'CallGetContact',
      annotations: { groupId: 'provider', groupLabel: 'Keap', explanation: 'Read contact profile from Keap.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteKeap'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'KeapContactReadTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetContactInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the contact profile result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [contactAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the contact profile and complete the Flow."
class CompleteContactRead implements Step<QueryResult<ContactProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteKeap'; }
  /** Persist and return the contact profile. */
  execute(context: Context, result: QueryResult<ContactProfile>): StepDecision {
    contactAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
