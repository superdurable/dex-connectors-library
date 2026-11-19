// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newCheckIPReputationStep, type Connection, type CheckIPReputationInput, type CheckIPReputationOutput } from '@superdurable/connector-threatbook';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'threatbook';
const resultAttribute = new Attribute<CheckIPReputationOutput>('threatbook-result', jsonCodec<CheckIPReputationOutput>());

/** Query ThreatBook through a typed connector Step. */
export class ThreatBookInspectionFlow implements Flow<CheckIPReputationInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ThreatBook connection name differs');
    this.read = newCheckIPReputationStep<CheckIPReputationInput>({
      stepType: 'CallCheckIPReputation',
      annotations: { groupId: 'provider', groupLabel: 'ThreatBook', explanation: 'Read provider data through ThreatBook.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteThreatBook'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ThreatBookInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<CheckIPReputationInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<CheckIPReputationOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteThreatBook'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<CheckIPReputationOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
