// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRoomsStep, type Connection, type ListRoomsOutput, type ListRoomsInput } from '../../src/index.js';

/** Static Dex Web connection for the Likelic example. */
export const ConnectionName = 'likelic';
const roomsResult = new Attribute<ListRoomsOutput>('likelic-rooms-status-result', jsonCodec<ListRoomsOutput>());

/** Read one page of notification rooms through Likelic. */
export class RoomListFlow implements Flow<ListRoomsInput> {
  private readonly start = new StartRooms();
  private readonly complete = new CompleteRooms();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Likelic connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'LikelicRoomList'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListRoomsInput> {
    return StepList.startStep(this.start).otherSteps(newListRoomsStep({
      stepType: 'CallLikelicRooms',
      annotations: { groupId: 'likelic', groupLabel: 'Likelic', explanation: 'Read the current state of one page of notification rooms.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteLikelicRooms'),
    }), this.complete);
  }
  /** Register the durable rooms observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [roomsResult] }; }
}

// dex:group group-id:likelic group-label:"Likelic"
// dex:explanation text:"Start the rooms-status query."
class StartRooms implements Step<ListRoomsInput> {
  getStepType(): string { return 'StartLikelicRooms'; }
  execute(_context: Context, input: ListRoomsInput): StepDecision { return goTo(new StepRef('CallLikelicRooms'), input); }
}

// dex:group group-id:likelic group-label:"Likelic"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteRooms implements Step<QueryResult<ListRoomsOutput>> {
  getStepType(): string { return 'CompleteLikelicRooms'; }
  execute(context: Context, result: QueryResult<ListRoomsOutput>): StepDecision { roomsResult.set(context, result.value); return gracefulComplete(result.value); }
}
