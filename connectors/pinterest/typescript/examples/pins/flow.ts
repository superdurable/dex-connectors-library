// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListPinsStep, type Connection, type ListPinsInput, type PinsPage } from '@superdurable/connector-pinterest';

/** Static Dex Web connection for the Pins example. */
export const ConnectionName = 'pinterest';
const resultAttribute = new Attribute<PinsPage>('pinterest-typescript-page', jsonCodec<PinsPage>());

/** List pins visible to the authorized Pinterest customer. */
export class PinsFlow implements Flow<ListPinsInput> {
  private readonly complete = new CompletePins();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Pins connection name differs');
    this.read = newListPinsStep<ListPinsInput>({
      stepType: 'CallListPins',
      annotations: { groupId: 'provider', groupLabel: 'Pinterest', explanation: 'List pins available to the authorized Pinterest customer.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PinterestPinsListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListPinsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the pins and complete the Flow."
class CompletePins implements Step<QueryResult<PinsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<PinsPage>>();
  getStepType(): string { return 'CompletePins'; }
  execute(context: Context, result: QueryResult<PinsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
