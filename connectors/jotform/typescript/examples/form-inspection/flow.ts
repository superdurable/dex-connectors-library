// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListFormsStep, type Connection, type ListFormsInput, type FormsPage } from '@superdurable/connector-jotform';

/** Static Dex Web connection for the Jotform example. */
export const ConnectionName = 'jotform';
const resultAttribute = new Attribute<FormsPage>('jotform-forms-page', jsonCodec<FormsPage>());

/** List forms visible to the connected Jotform key. */
export class FormInspectionFlow implements Flow<ListFormsInput> {
  private readonly complete = new CompleteFormInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Jotform connection name differs');
    this.read = newListFormsStep<ListFormsInput>({
      stepType: 'CallListForms',
      annotations: { groupId: 'provider', groupLabel: 'Jotform', explanation: 'List form IDs and titles accessible to the connected key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'JotformFormInspection'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListFormsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the form page and complete the Flow."
class CompleteFormInspection implements Step<QueryResult<FormsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<FormsPage>>();
  getStepType(): string { return 'CompleteListForms'; }
  execute(context: Context, result: QueryResult<FormsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
