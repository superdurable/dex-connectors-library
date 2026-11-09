// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newSearchPartnersStep, type Connection, type PartnerSearchPage, type SearchPartnersInput,
} from '@superdurable/connector-odoo';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'odoo';
const partnersAttribute = new Attribute<PartnerSearchPage>('odoo-partner-search', jsonCodec<PartnerSearchPage>());

/** Search company partners through a typed Odoo Query. */
export class PartnerSearchFlow implements Flow<SearchPartnersInput> {
  private readonly complete = new CompletePartnerSearch();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Odoo connection name differs');
    this.read = newSearchPartnersStep<SearchPartnersInput>({
      stepType: 'CallSearchPartners',
      annotations: { groupId: 'provider', groupLabel: 'Odoo', explanation: 'Search Odoo company partners by name pattern.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteOdoo'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'OdooPartnerSearchTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<SearchPartnersInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the partner search result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [partnersAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the partner search result and complete the Flow."
class CompletePartnerSearch implements Step<QueryResult<PartnerSearchPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOdoo'; }
  /** Persist and return the partner search result. */
  execute(context: Context, result: QueryResult<PartnerSearchPage>): StepDecision {
    partnersAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
