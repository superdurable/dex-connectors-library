// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetGlobalQuoteStep, type Connection, type GlobalQuoteOutput, type StockInput } from '../../src/index.js';

/** Static Dex Web connection name for the runnable example. */
export const ConnectionName = 'alphavantage';

/** One quote lookup with optional error branches left to fail the Flow. */
export class AlphaVantageQuoteDemo implements Flow<StockInput> {
  private readonly quoteFound = new QuoteFound();
  private readonly getQuote;

  /** Bind the real or scripted Alpha Vantage connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('demo requires the static alphavantage connection');
    this.getQuote = newGetGlobalQuoteStep<StockInput>({
      stepType: 'GetGlobalQuote',
      annotations: { groupId: 'alphavantage', groupLabel: 'Alpha Vantage', explanation: 'Read the latest available quote for the supplied stock symbol.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.quoteFound,
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'AlphaVantageQuoteDemo'; }
  /** Register the provider and terminal Steps. */ getSteps(): StepList<StockInput> { return StepList.startStep<StockInput>(this.getQuote).otherSteps(this.quoteFound); }
  /** The demo keeps no Attributes or Streams. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

// dex:group group-id:alphavantage group-label:"Alpha Vantage"
// dex:explanation text:"Complete the Flow with the validated global quote returned by Alpha Vantage."
class QuoteFound implements Step<QueryResult<GlobalQuoteOutput>> {
  getStepType(): string { return 'QuoteFound'; }
  execute(_context: Context, result: QueryResult<GlobalQuoteOutput>): StepDecision { return gracefulComplete(result.value); }
}
