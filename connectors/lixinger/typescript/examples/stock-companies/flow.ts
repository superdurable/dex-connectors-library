// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListStockCompaniesStep, type Connection, type ListStockCompaniesOutput, type ListStockCompaniesInput } from '../../src/index.js';

/** Static Dex Web connection for the Lixinger example. */
export const ConnectionName = 'lixinger';
const companiesResult = new Attribute<ListStockCompaniesOutput>('lixinger-stock-companies-result', jsonCodec<ListStockCompaniesOutput>());

/** Read one city's current companies through Lixinger. */
export class StockCompaniesFlow implements Flow<ListStockCompaniesInput> {
  private readonly start = new StartCompanies();
  private readonly complete = new CompleteCompanies();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Lixinger connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'LixingerStockCompanies'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListStockCompaniesInput> {
    return StepList.startStep(this.start).otherSteps(newListStockCompaniesStep({
      stepType: 'CallLixingerCompanies',
      annotations: { groupId: 'lixinger', groupLabel: 'Lixinger', explanation: 'Read the current observations for one city.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteLixingerCompanies'),
    }), this.complete);
  }
  /** Register the durable companies observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [companiesResult] }; }
}

// dex:group group-id:lixinger group-label:"Lixinger"
// dex:explanation text:"Start the stock-companies query."
class StartCompanies implements Step<ListStockCompaniesInput> {
  getStepType(): string { return 'StartLixingerCompanies'; }
  execute(_context: Context, input: ListStockCompaniesInput): StepDecision { return goTo(new StepRef('CallLixingerCompanies'), input); }
}

// dex:group group-id:lixinger group-label:"Lixinger"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteCompanies implements Step<QueryResult<ListStockCompaniesOutput>> {
  getStepType(): string { return 'CompleteLixingerCompanies'; }
  execute(context: Context, result: QueryResult<ListStockCompaniesOutput>): StepDecision { companiesResult.set(context, result.value); return gracefulComplete(result.value); }
}
