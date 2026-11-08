// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAdStep, type Connection, type GetAdInput, type AdMetadata } from '@superdurable/connector-meta-ads-insights';

/** Static Dex Web connection for the Meta Ads Insights example. */
export const ConnectionName = 'meta-ads-insights';
const resultAttribute = new Attribute<AdMetadata>('meta-ads-insights-ad-metadata', jsonCodec<AdMetadata>());

/** Read ad identity and delivery status using the Marketing API token. */
export class AdInspectionFlow implements Flow<GetAdInput> {
  private readonly complete = new CompleteAdInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Meta Ads Insights connection name differs');
    this.read = newGetAdStep<GetAdInput>({
      stepType: 'CallGetAd',
      annotations: { groupId: 'provider', groupLabel: 'Meta Ads Insights', explanation: 'Read the selected ad name and delivery status.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'MetaAdsInsightsAdInspectionTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetAdInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the ad metadata and complete the Flow."
class CompleteAdInspection implements Step<QueryResult<AdMetadata>> {
  readonly inputCodec = jsonCodec<QueryResult<AdMetadata>>();
  getStepType(): string { return 'CompleteGetAd'; }
  execute(context: Context, result: QueryResult<AdMetadata>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
