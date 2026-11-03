// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListZonesStep, type Connection, type ListZonesInput, type ZonesPage } from '@superdurable/connector-cloudflare';

/** Static Dex Web connection used by the Worker and starter. */
export const ConnectionName = 'cloudflare';
/** Safe request retained for Flow inspection. */
export const lookupRequest = new Attribute<ListZonesInput>('cloudflare-lookup-request', jsonCodec<ListZonesInput>());
/** Provider result retained for Flow inspection. */
export const lookupOutput = new Attribute<ZonesPage>('cloudflare-lookup-output', jsonCodec<ZonesPage>());

/** List the first Cloudflare zone page through one connector Step. */
export class CloudflareLookupFlow implements Flow<ListZonesInput> {
  private readonly begin = new BeginLookup();
  private readonly complete = new CompleteLookup();

  /** Bind the static connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Cloudflare connection is required');
  }
  /** Stable Flow type. */ getFlowType(): string { return 'CloudflareTypeScriptLookup'; }
  /** Register start, provider, and completion Steps. */
  getSteps(): StepList<ListZonesInput> {
    return StepList.startStep(this.begin).otherSteps(
      newListZonesStep({
        stepType: 'ListCloudflareZones',
        annotations: { groupId: 'cloudflare', groupLabel: 'Cloudflare', explanation: 'List the first page of Cloudflare zones.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [lookupRequest, lookupOutput] }; }
}

/** Record the request and enter the provider Step. */
// dex:group group-id:cloudflare group-label:"Cloudflare"
// dex:explanation text:"Start the Cloudflare lookup with its typed request."
export class BeginLookup implements Step<ListZonesInput> {
  /** Stable Step type. */ getStepType(): string { return 'BeginCloudflareTypeScriptLookup'; }
  /** Begin one zone lookup. */
  execute(context: Context, input: ListZonesInput): StepDecision {
    lookupRequest.set(context, input);
    return goTo(new StepRef('ListCloudflareZones'), input);
  }
}

/** Persist the returned zone page and complete. */
// dex:group group-id:cloudflare group-label:"Cloudflare"
// dex:explanation text:"Complete with the first returned Cloudflare zone page."
export class CompleteLookup implements Step<QueryResult<ZonesPage>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteCloudflareTypeScriptLookup'; }
  /** Return only the typed provider result. */
  execute(context: Context, result: QueryResult<ZonesPage>): StepDecision {
    lookupOutput.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
