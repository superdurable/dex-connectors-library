// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newLookupAreaCodesStep, type Connection, type LookupAreaCodesInput, type AreaCodeMatches } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'showapi';
const resultAttribute = new Attribute<AreaCodeMatches>('showapi-result', jsonCodec<AreaCodeMatches>());

/** Query ShowAPI through a typed connector Step. */
export class ShowAPIInspectionFlow implements Flow<LookupAreaCodesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ShowAPI connection name differs');
    this.read = newLookupAreaCodesStep<LookupAreaCodesInput>({
      stepType: 'CallLookupAreaCodes',
      annotations: { groupId: 'provider', groupLabel: 'ShowAPI', explanation: 'Read provider data through ShowAPI.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteShowAPI'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ShowAPIInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<LookupAreaCodesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<AreaCodeMatches>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteShowAPI'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<AreaCodeMatches>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
