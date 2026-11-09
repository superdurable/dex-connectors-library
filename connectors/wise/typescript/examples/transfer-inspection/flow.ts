// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetTransferStep, type Connection, type TransferStatus, type GetTransferInput,
} from '@superdurable/connector-wise';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'wise';
const transferAttribute = new Attribute<TransferStatus>('wise-transfer', jsonCodec<TransferStatus>());

/** Read one transfer through a typed Wise Query. */
export class TransferInspectionFlow implements Flow<GetTransferInput> {
  private readonly complete = new CompleteTransferInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Wise connection name differs');
    this.read = newGetTransferStep<GetTransferInput>({
      stepType: 'CallGetTransfer',
      annotations: { groupId: 'provider', groupLabel: 'Wise', explanation: 'Read one transfer from Wise.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteWise'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'WiseTransferInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetTransferInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected transfer result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [transferAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected transfer and complete the Flow."
class CompleteTransferInspection implements Step<QueryResult<TransferStatus>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWise'; }
  /** Persist and return the selected transfer. */
  execute(context: Context, result: QueryResult<TransferStatus>): StepDecision {
    transferAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
