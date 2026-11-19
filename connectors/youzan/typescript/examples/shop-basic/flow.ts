// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetShopBasicStep, type Connection, type GetShopBasicInput, type ShopBasic } from '@superdurable/connector-youzan';

/** Static Dex Web connection name. */
export const ConnectionName = 'youzan';
/** Durable shop metadata without credentials. */
export const shopAttribute = new Attribute('youzan-shop', jsonCodec<ShopBasic>());

/** Look up one Youzan shop. */
export class YouzanShopBasicFlow implements Flow<GetShopBasicInput> {
  private readonly complete = new ShopFound();
  private readonly lookup;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('shop Flow requires the youzan connection');
    this.lookup = newGetShopBasicStep<GetShopBasicInput>({
      stepType: 'GetYouzanShop',
      annotations: { groupId: 'youzan', groupLabel: 'Youzan', explanation: 'Read the shop authorized by the connection token.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteYouzanShop'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'YouzanShopBasic'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<GetShopBasicInput> { return StepList.startStep(this.lookup).otherSteps(this.complete); }
  /** Register the persisted metadata. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [shopAttribute] }; }
}

// dex:group group-id:youzan group-label:"Youzan"
// dex:explanation text:"Persist shop metadata and complete the Flow."
class ShopFound implements Step<QueryResult<ShopBasic>> {
  getStepType(): string { return 'CompleteYouzanShop'; }
  execute(context: Context, result: QueryResult<ShopBasic>): StepDecision {
    shopAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
