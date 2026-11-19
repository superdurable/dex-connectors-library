// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSegmentTextStep, type Connection, type SegmentTextInput, type SegmentTextOutput } from '@superdurable/connector-gugudata';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'gugudata';
const resultAttribute = new Attribute<SegmentTextOutput>('gugudata-result', jsonCodec<SegmentTextOutput>());

/** Query GuGuData through a typed connector Step. */
export class GuGuDataInspectionFlow implements Flow<SegmentTextInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('GuGuData connection name differs');
    this.read = newSegmentTextStep<SegmentTextInput>({
      stepType: 'CallSegmentText',
      annotations: { groupId: 'provider', groupLabel: 'GuGuData', explanation: 'Read provider data through GuGuData.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      segmented: new StepRef('CompleteGuGuData'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GuGuDataInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SegmentTextInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SegmentTextOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGuGuData'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SegmentTextOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
