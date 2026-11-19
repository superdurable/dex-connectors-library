// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newLookupAppIDStep, type Connection, type LookupAppIDInput, type LookupAppIDOutput } from '@superdurable/connector-honor';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'honor';
const resultAttribute = new Attribute<LookupAppIDOutput>('honor-result', jsonCodec<LookupAppIDOutput>());

/** Query Honor through a typed connector Step. */
export class HonorInspectionFlow implements Flow<LookupAppIDInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Honor connection name differs');
    this.read = newLookupAppIDStep<LookupAppIDInput>({
      stepType: 'CallLookupAppID',
      annotations: { groupId: 'provider', groupLabel: 'Honor', explanation: 'Read provider data through Honor.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteHonor'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'HonorInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<LookupAppIDInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<LookupAppIDOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteHonor'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<LookupAppIDOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
