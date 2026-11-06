// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchObjectsStep, type Connection, type ObjectPage, type SearchObjectsInput } from '@superdurable/connector-hubspot';

/** Static HubSpot connection selected in Dex Web. */
export const ConnectionName = 'hubspot-crm';
/** Persisted nonsecret search page. */
export const searchPage = new Attribute<ObjectPage>('hubspot-search-page', jsonCodec<ObjectPage>());

/** Search one bounded page of HubSpot deals. */
export class SearchDealsFlow implements Flow<SearchObjectsInput> {
  private readonly prepare = new PrepareSearchDeals();
  private readonly complete = new CompleteSearchDeals();
  /** Bind the static project connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'HubSpotSearchDealsFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<SearchObjectsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchObjectsStep({
        stepType: 'SearchHubSpotDeals',
        annotations: { groupId: 'hubspot-crm', groupLabel: 'HubSpot CRM', explanation: 'Search one bounded page of deals.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        searched: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [searchPage] }; }
}

// dex:group group-id:hubspot-crm group-label:"HubSpot CRM"
// dex:explanation text:"Validate the deal search before invoking HubSpot."
/** Reject non-deal input before dispatch. */
export class PrepareSearchDeals implements Step<SearchObjectsInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareHubSpotSearchDeals'; }
  /** Start the provider query. */ execute(_context: Context, input: SearchObjectsInput): StepDecision {
    if (input.objectType !== 'deals') return forceFail('Expected a HubSpot deals search');
    return goTo(new StepRef('SearchHubSpotDeals'), input);
  }
}

// dex:group group-id:hubspot-crm group-label:"HubSpot CRM"
// dex:explanation text:"Persist the bounded deal page and complete the Flow."
/** Complete with the safe search page. */
export class CompleteSearchDeals implements Step<QueryResult<ObjectPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteHubSpotSearchDeals'; }
  /** Persist the provider result. */ execute(context: Context, result: QueryResult<ObjectPage>): StepDecision {
    searchPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
