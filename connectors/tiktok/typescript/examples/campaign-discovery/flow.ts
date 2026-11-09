// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListCampaignsStep, type Connection, type CampaignsPage, type ListCampaignsInput,
} from '@superdurable/connector-tiktok-business';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tiktok-business';
const campaignsAttribute = new Attribute<CampaignsPage>('tiktok-campaigns', jsonCodec<CampaignsPage>());

/** Read one campaign page through a typed TikTok Query. */
export class CampaignDiscoveryFlow implements Flow<ListCampaignsInput> {
  private readonly complete = new CompleteCampaignDiscovery();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TikTok connection name differs');
    this.read = newListCampaignsStep<ListCampaignsInput>({
      stepType: 'CallListCampaigns',
      annotations: { groupId: 'provider', groupLabel: 'TikTok', explanation: 'Read one campaign page from TikTok.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteTikTok'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'TikTokCampaignDiscoveryTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListCampaignsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected campaign page result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [campaignsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected campaign page and complete the Flow."
class CompleteCampaignDiscovery implements Step<QueryResult<CampaignsPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTikTok'; }
  /** Persist and return the selected campaign page. */
  execute(context: Context, result: QueryResult<CampaignsPage>): StepDecision {
    campaignsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
