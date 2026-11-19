// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newCountClassObjectsStep, type Connection, type CountClassObjectsInput, type CountClassObjectsOutput } from '@superdurable/connector-bmob';

/** Static Dex Web connection name. */
export const ConnectionName = 'bmob';
/** Durable public class count. */
export const classCountAttribute = new Attribute('bmob-class-count', jsonCodec<CountClassObjectsOutput>());

/** Count objects in one Bmob class without exposing records. */
export class BmobClassCountFlow implements Flow<CountClassObjectsInput> {
  private readonly completed = new ClassCounted();
  private readonly lookup;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('class count Flow requires the bmob connection');
    this.lookup = newCountClassObjectsStep<CountClassObjectsInput>({
      stepType: 'CountBmobClassObjects',
      annotations: { groupId: 'bmob', groupLabel: 'Bmob', explanation: 'Request the count of the selected Bmob class without reading its records.' },
      connection, connectionName: ConnectionName, mapToOperationInput: request => request,
      counted: new StepRef('ClassCounted'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'BmobCountClassObjectsOutput'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<CountClassObjectsInput> { return StepList.startStep(this.lookup).otherSteps(this.completed); }
  /** Register the persisted class count. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [classCountAttribute] }; }
}

// dex:group group-id:bmob group-label:"Bmob"
// dex:explanation text:"Persist the class count and complete the Flow."
class ClassCounted implements Step<QueryResult<CountClassObjectsOutput>> {
  getStepType(): string { return 'ClassCounted'; }
  execute(context: Context, result: QueryResult<CountClassObjectsOutput>): StepDecision {
    classCountAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
