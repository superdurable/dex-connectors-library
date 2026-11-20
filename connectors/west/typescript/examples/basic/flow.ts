// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDomainsStep, type Connection, type ListDomainsInput, type ListDomainsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'west';
const resultAttribute = new Attribute<ListDomainsOutput>('west-result', jsonCodec<ListDomainsOutput>());

/** Query West through a typed connector Step. */
export class WestListDomainsFlow implements Flow<ListDomainsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('West connection name differs');
    this.read = newListDomainsStep<ListDomainsInput>({
      stepType: 'CallListDomains',
      annotations: { groupId: 'provider', groupLabel: 'West', explanation: 'Read provider data through West.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteWest'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WestListDomainsTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListDomainsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListDomainsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWest'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListDomainsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
