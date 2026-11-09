// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetContactStep, type Connection, type ContactMetadata, type GetContactInput,
} from '@superdurable/connector-constantcontact';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'constantcontact';
const contactAttribute = new Attribute<ContactMetadata>('constantcontact-contact', jsonCodec<ContactMetadata>());

/** Read one contact through a typed Constant Contact Query. */
export class ContactInspectionFlow implements Flow<GetContactInput> {
  private readonly complete = new CompleteContactInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Constant Contact connection name differs');
    this.read = newGetContactStep<GetContactInput>({
      stepType: 'CallGetContact',
      annotations: { groupId: 'provider', groupLabel: 'Constant Contact', explanation: 'Read one contact from Constant Contact.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteConstantContact'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'ConstantContactContactInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetContactInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected contact result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [contactAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected contact and complete the Flow."
class CompleteContactInspection implements Step<QueryResult<ContactMetadata>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteConstantContact'; }
  /** Persist and return the selected contact. */
  execute(context: Context, result: QueryResult<ContactMetadata>): StepDecision {
    contactAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
