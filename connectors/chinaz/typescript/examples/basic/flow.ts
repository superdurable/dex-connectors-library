// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetSiteTDKStep, type Connection, type GetSiteTDKInput, type SiteTDK } from '@superdurable/connector-chinaz';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'chinaz';
const resultAttribute = new Attribute<SiteTDK>('chinaz-result', jsonCodec<SiteTDK>());

/** Query Chinaz through a typed connector Step. */
export class ChinazInspectionFlow implements Flow<GetSiteTDKInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Chinaz connection name differs');
    this.read = newGetSiteTDKStep<GetSiteTDKInput>({
      stepType: 'CallGetSiteTDK',
      annotations: { groupId: 'provider', groupLabel: 'Chinaz', explanation: 'Read provider data through Chinaz.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteChinaz'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ChinazInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetSiteTDKInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SiteTDK>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteChinaz'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SiteTDK>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
