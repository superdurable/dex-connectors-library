// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListQuotasStep, type Connection, type ListQuotasInput, type QuotaPage } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'zhihu';
const resultAttribute = new Attribute<QuotaPage>('zhihu-result', jsonCodec<QuotaPage>());

/** Query Zhihu through a typed connector Step. */
export class ZhihuInspectionFlow implements Flow<ListQuotasInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Zhihu connection name differs');
    this.read = newListQuotasStep<ListQuotasInput>({
      stepType: 'CallListQuotas',
      annotations: { groupId: 'provider', groupLabel: 'Zhihu', explanation: 'Read provider data through Zhihu.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteZhihu'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ZhihuInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListQuotasInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<QuotaPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteZhihu'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<QuotaPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
