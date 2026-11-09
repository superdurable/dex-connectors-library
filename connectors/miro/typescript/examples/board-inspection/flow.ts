// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetBoardStep, type Connection, type BoardMetadata, type GetBoardInput,
} from '@superdurable/connector-miro';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'miro';
const boardAttribute = new Attribute<BoardMetadata>('miro-board', jsonCodec<BoardMetadata>());

/** Read one board through a typed Miro Query. */
export class BoardInspectionFlow implements Flow<GetBoardInput> {
  private readonly complete = new CompleteBoardInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Miro connection name differs');
    this.read = newGetBoardStep<GetBoardInput>({
      stepType: 'CallGetBoard',
      annotations: { groupId: 'provider', groupLabel: 'Miro', explanation: 'Read one board from Miro.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteMiro'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'MiroBoardInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetBoardInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected board result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [boardAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected board and complete the Flow."
class CompleteBoardInspection implements Step<QueryResult<BoardMetadata>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMiro'; }
  /** Persist and return the selected board. */
  execute(context: Context, result: QueryResult<BoardMetadata>): StepDecision {
    boardAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
