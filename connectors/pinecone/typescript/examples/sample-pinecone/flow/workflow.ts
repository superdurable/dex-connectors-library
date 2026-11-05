// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newUpsertVectorsStep, type Connection, type UpsertVectorsInput, type UpsertVectorsOutput } from '@superdurable/connector-pinecone';

/** Static Dex Web connection name. */
export const ConnectionName = 'pinecone';
/** Safe accepted count persisted by the sample. */
export const upsertResult = new Attribute<UpsertVectorsOutput>('pinecone-sample-result', jsonCodec<UpsertVectorsOutput>());

/** Upsert a bounded batch into one Pinecone index. */
export class PineconeSampleFlow implements Flow<UpsertVectorsInput> {
  private readonly start = new StartRequest();
  private readonly complete = new CompleteRequest();
  /** Bind the static Pinecone connection. */
  constructor(private readonly connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('Pinecone sample requires the static pinecone connection');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PineconeSample'; }
  /** Register the source, provider Step, and completion. */
  getSteps(): StepList<UpsertVectorsInput> {
    return StepList.startStep(this.start).otherSteps(newUpsertVectorsStep({
      stepType: 'CallPinecone',
      annotations: { groupId: 'pinecone', groupLabel: 'Pinecone', explanation: 'Upsert vector IDs by absolute replacement in one namespace.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      upserted: this.complete,
    }), this.complete);
  }
  /** Register the persisted result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [upsertResult] }; }
}

// dex:group group-id:pinecone-start group-label:"Pinecone sample"
// dex:explanation text:"Pass the bounded vector batch to Pinecone."
/** Start the provider mutation. */
export class StartRequest implements Step<UpsertVectorsInput> {
  /** Stable Step identity. */ getStepType(): string { return 'StartPineconeRequest'; }
  /** Route the validated input. */ execute(_context: Context, input: UpsertVectorsInput): StepDecision { return goTo(new StepRef('CallPinecone'), input); }
}

// dex:group group-id:pinecone-complete group-label:"Pinecone sample"
// dex:explanation text:"Persist the accepted count and complete the Flow."
/** Finish with the provider's accepted count. */
export class CompleteRequest implements Step<MutationResult<UpsertVectorsOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompletePineconeRequest'; }
  /** Persist a safe provider result. */
  execute(context: Context, result: MutationResult<UpsertVectorsOutput>): StepDecision {
    upsertResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
