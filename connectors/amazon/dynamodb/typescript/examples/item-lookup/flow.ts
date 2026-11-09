// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetItemStep, type Connection, type ItemResult, type GetItemInput,
} from '@superdurable/connector-amazon-dynamodb';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'amazon-dynamodb';
const functionDetailsAttribute = new Attribute<ItemResult>('dynamodb-item-lookup', jsonCodec<ItemResult>());

/** Read one item through a typed Amazon DynamoDB Query. */
export class ItemLookupFlow implements Flow<GetItemInput> {
  private readonly complete = new CompleteItemResult();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amazon DynamoDB connection name differs');
    this.read = newGetItemStep<GetItemInput>({
      stepType: 'CallGetItem',
      annotations: { groupId: 'provider', groupLabel: 'Amazon DynamoDB', explanation: 'Read one item from Amazon DynamoDB.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGetItem'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'AmazonDynamoDBItemLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetItemInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected item result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [functionDetailsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected item and complete the Flow."
class CompleteItemResult implements Step<QueryResult<ItemResult>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetItem'; }
  /** Persist and return the selected item. */
  execute(context: Context, result: QueryResult<ItemResult>): StepDecision {
    functionDetailsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
