// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetKeyStep, type Connection, type KeyLookup, type GetKeyInput,
} from '@superdurable/connector-upstash-redis';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'upstash-redis';
const keyLookupAttribute = new Attribute<KeyLookup>('redis-key-lookup', jsonCodec<KeyLookup>());

/** Read one key value through a typed Upstash Redis Query. */
export class KeyLookupFlow implements Flow<GetKeyInput> {
  private readonly complete = new CompleteKeyLookup();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Upstash Redis connection name differs');
    this.read = newGetKeyStep<GetKeyInput>({
      stepType: 'CallGetKey',
      annotations: { groupId: 'provider', groupLabel: 'Upstash Redis', explanation: 'Read one key value from Upstash Redis.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGetKey'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'UpstashRedisKeyLookupTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetKeyInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected key value result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [keyLookupAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected key value and complete the Flow."
class CompleteKeyLookup implements Step<QueryResult<KeyLookup>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetKey'; }
  /** Persist and return the selected key value. */
  execute(context: Context, result: QueryResult<KeyLookup>): StepDecision {
    keyLookupAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
