// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListCampaignsStep, type Connection, type ListCampaignsInput, type CampaignsPage } from '@superdurable/connector-microsoft-advertising';

/** Static Dex Web connection for campaign discovery. */
export const ConnectionName = 'microsoft-advertising';
const resultAttribute = new Attribute<CampaignsPage>('microsoft-advertising-typescript-campaigns', jsonCodec<CampaignsPage>());

/** List campaigns visible to the authorized Microsoft Advertising user. */
export class CampaignFlow implements Flow<ListCampaignsInput> {
  private readonly complete = new CompleteCampaignDiscovery();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Microsoft Advertising connection name differs');
    this.read = newListCampaignsStep<ListCampaignsInput>({
      stepType: 'CallListCampaigns',
      annotations: { groupId: 'provider', groupLabel: 'Microsoft Advertising', explanation: 'List campaigns in the selected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'MicrosoftAdvertisingCampaignDiscoveryTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListCampaignsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the campaign list and complete the Flow."
class CompleteCampaignDiscovery implements Step<QueryResult<CampaignsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<CampaignsPage>>();
  getStepType(): string { return 'CompleteCampaignDiscovery'; }
  execute(context: Context, result: QueryResult<CampaignsPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
