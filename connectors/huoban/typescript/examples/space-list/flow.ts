// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSpacesStep, type Connection, type ListSpacesInput, type ListSpacesOutput } from '../../src/index.js';

/** Static Dex Web connection for the Huoban example. */
export const ConnectionName = 'huoban';
const spacesResult = new Attribute<ListSpacesOutput>('huoban-space-list-result', jsonCodec<ListSpacesOutput>());

/** Read authorized workspace IDs and names through Huoban. */
export class SpaceCatalogFlow implements Flow<ListSpacesInput> {
  private readonly read;
  private readonly complete = new CompleteSpaceList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Huoban connection name differs');
    this.read = newListSpacesStep<ListSpacesInput>({
      stepType: 'CallHuoban',
      annotations: { groupId: 'huoban', groupLabel: 'Huoban', explanation: 'Read authorized workspaces with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteHuobanSpaces'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'HuobanSpaceCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListSpacesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable workspace list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [spacesResult] }; }
}

// dex:group group-id:huoban group-label:"Huoban"
// dex:explanation text:"Persist the workspace list and complete the Flow."
class CompleteSpaceList implements Step<QueryResult<ListSpacesOutput>> {
  getStepType(): string { return 'CompleteHuobanSpaces'; }
  execute(context: Context, result: QueryResult<ListSpacesOutput>): StepDecision { spacesResult.set(context, result.value); return gracefulComplete(result.value); }
}
