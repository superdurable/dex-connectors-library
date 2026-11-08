// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPropertyStep, type Connection, type GetPropertyInput, type Property } from '@superdurable/connector-google-analytics';

/** Static Dex Web connection for the Analytics 4 example. */
export const ConnectionName = 'google-analytics-4';
const resultAttribute = new Attribute<Property>('google-analytics-4-property-page', jsonCodec<Property>());

/** List property visible to the connected Analytics 4 user. */
export class PropertyFlow implements Flow<GetPropertyInput> {
  private readonly complete = new CompleteProperty();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Analytics 4 connection name differs');
    this.read = newGetPropertyStep<GetPropertyInput>({
      stepType: 'CallGetProperty',
      annotations: { groupId: 'provider', groupLabel: 'Analytics 4', explanation: 'Read one Analytics 4 property using Google OAuth.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleAnalytics4PropertyTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetPropertyInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the property and complete the Flow."
class CompleteProperty implements Step<QueryResult<Property>> {
  readonly inputCodec = jsonCodec<QueryResult<Property>>();
  getStepType(): string { return 'CompleteProperty'; }
  execute(context: Context, result: QueryResult<Property>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
