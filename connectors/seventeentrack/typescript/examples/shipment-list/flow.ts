// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTrackedShipmentsStep, type Connection, type ListTrackedShipmentsInput, type TrackedShipmentPage } from '@superdurable/connector-seventeentrack';

/** Static Dex Web connection for the SeventeenTrack example. */
export const ConnectionName = 'seventeentrack';
const shipmentsResult = new Attribute<TrackedShipmentPage>('seventeentrack-model-list-result', jsonCodec<TrackedShipmentPage>());

/** Read the shipment list through SeventeenTrack. */
export class TrackedShipmentsFlow implements Flow<ListTrackedShipmentsInput> {
  private readonly read;
  private readonly complete = new CompleteShipmentList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SeventeenTrack connection name differs');
    this.read = newListTrackedShipmentsStep<ListTrackedShipmentsInput>({
      stepType: 'CallSeventeenTrack',
      annotations: { groupId: 'seventeentrack', groupLabel: 'SeventeenTrack', explanation: 'Read the shipment status with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteSeventeenTrackShipments'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'SeventeenTrackShipmentList'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListTrackedShipmentsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable shipment list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [shipmentsResult] }; }
}

// dex:group group-id:seventeentrack group-label:"SeventeenTrack"
// dex:explanation text:"Persist the shipment list and complete the Flow."
class CompleteShipmentList implements Step<QueryResult<TrackedShipmentPage>> {
  getStepType(): string { return 'CompleteSeventeenTrackShipments'; }
  execute(context: Context, result: QueryResult<TrackedShipmentPage>): StepDecision { shipmentsResult.set(context, result.value); return gracefulComplete(result.value); }
}
