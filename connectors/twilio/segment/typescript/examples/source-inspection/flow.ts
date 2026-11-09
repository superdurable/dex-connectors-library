// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetSourceStep, type Connection, type SourceProfile, type GetSourceInput,
} from '@superdurable/connector-twilio-segment';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'segment';
const sourceAttribute = new Attribute<SourceProfile>('segment-source', jsonCodec<SourceProfile>());

/** Read one source through a typed Segment Query. */
export class SourceInspectionFlow implements Flow<GetSourceInput> {
  private readonly complete = new CompleteSourceInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Segment connection name differs');
    this.read = newGetSourceStep<GetSourceInput>({
      stepType: 'CallGetSource',
      annotations: { groupId: 'provider', groupLabel: 'Segment', explanation: 'Read one source from Segment.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteSegment'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'SegmentSourceInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetSourceInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected source result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [sourceAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected source and complete the Flow."
class CompleteSourceInspection implements Step<QueryResult<SourceProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSegment'; }
  /** Persist and return the selected source. */
  execute(context: Context, result: QueryResult<SourceProfile>): StepDecision {
    sourceAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
