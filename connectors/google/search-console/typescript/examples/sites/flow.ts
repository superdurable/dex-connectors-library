// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSitesStep, type Connection, type ListSitesInput, type SitesPage } from '@superdurable/connector-google-search-console';

/** Static Dex Web connection for the Search Console example. */
export const ConnectionName = 'google-search-console';
const resultAttribute = new Attribute<SitesPage>('google-search-console-sites-page', jsonCodec<SitesPage>());

/** List sites visible to the connected Search Console user. */
export class SitesFlow implements Flow<ListSitesInput> {
  private readonly complete = new CompleteSites();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Search Console connection name differs');
    this.read = newListSitesStep<ListSitesInput>({
      stepType: 'CallListSites',
      annotations: { groupId: 'provider', groupLabel: 'Search Console', explanation: 'List accessible property URLs and permissions using Google OAuth.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleSearchConsoleSitesTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListSitesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the site page and complete the Flow."
class CompleteSites implements Step<QueryResult<SitesPage>> {
  readonly inputCodec = jsonCodec<QueryResult<SitesPage>>();
  getStepType(): string { return 'CompleteSites'; }
  execute(context: Context, result: QueryResult<SitesPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
