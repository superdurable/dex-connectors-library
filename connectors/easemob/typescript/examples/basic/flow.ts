// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetUserDetailStep, type Connection, type GetUserDetailInput, type UserDetail } from '@superdurable/connector-easemob';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'easemob';
const resultAttribute = new Attribute<UserDetail>('easemob-result', jsonCodec<UserDetail>());

/** Query Easemob through a typed connector Step. */
export class EasemobInspectionFlow implements Flow<GetUserDetailInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Easemob connection name differs');
    this.read = newGetUserDetailStep<GetUserDetailInput>({
      stepType: 'CallGetUserDetail',
      annotations: { groupId: 'provider', groupLabel: 'Easemob', explanation: 'Read provider data through Easemob.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteEasemob'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'EasemobInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetUserDetailInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<UserDetail>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteEasemob'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<UserDetail>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
