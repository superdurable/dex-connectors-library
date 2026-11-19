// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCompanyProfileStep, type Connection, type GetCompanyProfileInput, type GetCompanyProfileOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'yuandian';
const resultAttribute = new Attribute<GetCompanyProfileOutput>('yuandian-result', jsonCodec<GetCompanyProfileOutput>());

/** Query Yuandian through a typed connector Step. */
export class YuandianInspectionFlow implements Flow<GetCompanyProfileInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Yuandian connection name differs');
    this.read = newGetCompanyProfileStep<GetCompanyProfileInput>({
      stepType: 'CallGetCompanyProfile',
      annotations: { groupId: 'provider', groupLabel: 'Yuandian', explanation: 'Read provider data through Yuandian.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteYuandian'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'YuandianInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetCompanyProfileInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetCompanyProfileOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteYuandian'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetCompanyProfileOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
