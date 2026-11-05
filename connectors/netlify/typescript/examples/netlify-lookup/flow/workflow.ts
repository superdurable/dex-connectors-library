// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSitesStep, type Connection, type ListSitesInput, type SitesPage } from '@superdurable/connector-netlify';

/** Static Dex Web connection name. */
export const ConnectionName = 'netlify';
/** Safe request persisted by the example. */
export const lookupRequest = new Attribute<ListSitesInput>('netlify-lookup-request', jsonCodec<ListSitesInput>());
/** Safe result persisted by the example. */
export const lookupOutput = new Attribute<SitesPage>('netlify-lookup-output', jsonCodec<SitesPage>());
/** One Netlify site-list Flow. */
export class NetlifyLookupFlow implements Flow<ListSitesInput> {
  private readonly prepare = new PrepareNetlifyLookup();
  private readonly complete = new CompleteNetlifyLookup();
  /** Bind a named connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Netlify connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'NetlifyLookup'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<ListSitesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListSitesStep({
        stepType: 'ListSitesNetlify', annotations: { groupId: 'netlify', groupLabel: 'Netlify', explanation: 'List sites visible to the Netlify token.' },
        connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
        listed: this.complete,
      }), this.complete,
    );
  }
  /** Register persisted attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [lookupRequest, lookupOutput] }; }
}
/** Store the safe request and enter the provider Step. */
// dex:group group-id:netlify group-label:"Netlify"
// dex:explanation text:"Start the Netlify lookup with its typed request."
export class PrepareNetlifyLookup implements Step<ListSitesInput> {
  /** Stable Step identity. */ getStepType(): string { return 'StartNetlifyLookup'; }
  /** Save request and route. */ execute(context: Context, input: ListSitesInput): StepDecision { lookupRequest.set(context, input); return goTo(new StepRef('ListSitesNetlify'), input); }
}
/** Store the site list and complete. */
// dex:group group-id:netlify group-label:"Netlify"
// dex:explanation text:"Complete with the Netlify result."
export class CompleteNetlifyLookup implements Step<QueryResult<SitesPage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteNetlifyLookup'; }
  /** Save output and complete. */ execute(context: Context, result: QueryResult<SitesPage>): StepDecision { lookupOutput.set(context, result.value); return gracefulComplete(result.value); }
}
