// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSystemVoicesStep, type Connection, type ListSystemVoicesInput, type ListSystemVoicesOutput } from '@superdurable/connector-unisound';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'unisound';
const resultAttribute = new Attribute<ListSystemVoicesOutput>('unisound-result', jsonCodec<ListSystemVoicesOutput>());

/** Query Unisound through a typed connector Step. */
export class UnisoundInspectionFlow implements Flow<ListSystemVoicesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Unisound connection name differs');
    this.read = newListSystemVoicesStep<ListSystemVoicesInput>({
      stepType: 'CallListSystemVoices',
      annotations: { groupId: 'provider', groupLabel: 'Unisound', explanation: 'Read provider data through Unisound.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteUnisound'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'UnisoundInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListSystemVoicesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListSystemVoicesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteUnisound'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListSystemVoicesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
