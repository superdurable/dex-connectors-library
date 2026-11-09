// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetOpportunityStep, type Connection, type OpportunityStage, type GetOpportunityInput,
} from '@superdurable/connector-lever';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'lever';
const opportunityAttribute = new Attribute<OpportunityStage>('lever-opportunity', jsonCodec<OpportunityStage>());

/** Read one opportunity through a typed Lever Query. */
export class OpportunityInspectionFlow implements Flow<GetOpportunityInput> {
  private readonly complete = new CompleteOpportunityInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Lever connection name differs');
    this.read = newGetOpportunityStep<GetOpportunityInput>({
      stepType: 'CallGetOpportunity',
      annotations: { groupId: 'provider', groupLabel: 'Lever', explanation: 'Read one opportunity from Lever.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteLever'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'LeverOpportunityInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetOpportunityInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected opportunity result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [opportunityAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected opportunity and complete the Flow."
class CompleteOpportunityInspection implements Step<QueryResult<OpportunityStage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteLever'; }
  /** Persist and return the selected opportunity. */
  execute(context: Context, result: QueryResult<OpportunityStage>): StepDecision {
    opportunityAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
