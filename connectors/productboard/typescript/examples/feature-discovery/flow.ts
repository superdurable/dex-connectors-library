// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListFeaturesStep, type Connection, type FeaturesPage, type ListFeaturesInput,
} from '@superdurable/connector-productboard';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'productboard';
const featuresAttribute = new Attribute<FeaturesPage>('productboard-features', jsonCodec<FeaturesPage>());

/** Read one feature page through a typed Productboard Query. */
export class FeatureDiscoveryFlow implements Flow<ListFeaturesInput> {
  private readonly complete = new CompleteFeatureDiscovery();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Productboard connection name differs');
    this.read = newListFeaturesStep<ListFeaturesInput>({
      stepType: 'CallListFeatures',
      annotations: { groupId: 'provider', groupLabel: 'Productboard', explanation: 'Read one feature page from Productboard.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteProductboard'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'ProductboardFeatureDiscoveryTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListFeaturesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected feature page result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [featuresAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected feature page and complete the Flow."
class CompleteFeatureDiscovery implements Step<QueryResult<FeaturesPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteProductboard'; }
  /** Persist and return the selected feature page. */
  execute(context: Context, result: QueryResult<FeaturesPage>): StepDecision {
    featuresAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
