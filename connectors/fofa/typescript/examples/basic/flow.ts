// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchDomainAssetsStep, type Connection, type SearchDomainAssetsInput, type SearchDomainAssetsOutput } from '@superdurable/connector-fofa';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fofa';
const resultAttribute = new Attribute<SearchDomainAssetsOutput>('fofa-result', jsonCodec<SearchDomainAssetsOutput>());

/** Query Fofa through a typed connector Step. */
export class FofaInspectionFlow implements Flow<SearchDomainAssetsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Fofa connection name differs');
    this.read = newSearchDomainAssetsStep<SearchDomainAssetsInput>({
      stepType: 'CallSearchDomainAssets',
      annotations: { groupId: 'provider', groupLabel: 'Fofa', explanation: 'Read provider data through Fofa.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteFofa'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FofaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SearchDomainAssetsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SearchDomainAssetsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFofa'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SearchDomainAssetsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
