// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetLeadStep, type Connection, type LeadProfile, type GetLeadInput,
} from '@superdurable/connector-adobe-marketo';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'adobe-marketo';
const leadAttribute = new Attribute<LeadProfile>('adobe-marketo-lead', jsonCodec<LeadProfile>());

/** Read one lead through a typed Marketo Query. */
export class LeadInspectionFlow implements Flow<GetLeadInput> {
  private readonly complete = new CompleteLeadInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Marketo connection name differs');
    this.read = newGetLeadStep<GetLeadInput>({
      stepType: 'CallGetLead',
      annotations: { groupId: 'provider', groupLabel: 'Marketo', explanation: 'Read one lead from Marketo.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteMarketo'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MarketoLeadInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetLeadInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected lead result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [leadAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected lead and complete the Flow."
class CompleteLeadInspection implements Step<QueryResult<LeadProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMarketo'; }
  /** Persist and return the selected lead. */
  execute(context: Context, result: QueryResult<LeadProfile>): StepDecision {
    leadAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
