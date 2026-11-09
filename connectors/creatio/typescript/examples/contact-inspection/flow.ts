// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetContactStep, type Connection, type ContactSummary, type GetContactInput,
} from '@superdurable/connector-creatio';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'creatio';
const contactAttribute = new Attribute<ContactSummary>('creatio-contact', jsonCodec<ContactSummary>());

/** Read one contact through a typed Creatio Query. */
export class ContactInspectionFlow implements Flow<GetContactInput> {
  private readonly complete = new CompleteContactInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Creatio connection name differs');
    this.read = newGetContactStep<GetContactInput>({
      stepType: 'CallGetContact',
      annotations: { groupId: 'provider', groupLabel: 'Creatio', explanation: 'Read one contact from Creatio.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteCreatio'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'CreatioContactInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetContactInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected contact result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [contactAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected contact and complete the Flow."
class CompleteContactInspection implements Step<QueryResult<ContactSummary>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteCreatio'; }
  /** Persist and return the selected contact. */
  execute(context: Context, result: QueryResult<ContactSummary>): StepDecision {
    contactAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
