// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetShopBasicStep, type Connection, type GetShopBasicInput, type ShopBasic } from '@superdurable/connector-youzan';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'youzan';
const resultAttribute = new Attribute<ShopBasic>('youzan-result', jsonCodec<ShopBasic>());

/** Query Youzan through a typed connector Step. */
export class YouzanInspectionFlow implements Flow<GetShopBasicInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Youzan connection name differs');
    this.read = newGetShopBasicStep<GetShopBasicInput>({
      stepType: 'CallGetShopBasic',
      annotations: { groupId: 'provider', groupLabel: 'Youzan', explanation: 'Read provider data through Youzan.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteYouzan'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'YouzanInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetShopBasicInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ShopBasic>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteYouzan'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ShopBasic>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
