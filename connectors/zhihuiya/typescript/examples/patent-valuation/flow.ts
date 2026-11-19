// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPatentValuationStep, type Connection, type GetPatentValuationInput, type GetPatentValuationOutput } from '../../src/index.js';

/** Static Dex Web connection for the Zhihuiya example. */
export const ConnectionName = 'zhihuiya';
const valuationResult = new Attribute<GetPatentValuationOutput>('zhihuiya-patent-valuation-result', jsonCodec<GetPatentValuationOutput>());

/** Read the patent valuation through Zhihuiya. */
export class PatentValuationFlow implements Flow<GetPatentValuationInput> {
  private readonly start = new StartPatentValuation();
  private readonly complete = new CompletePatentValuation();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Zhihuiya connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ZhihuiyaPatentValuation'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetPatentValuationInput> {
    return StepList.startStep(this.start).otherSteps(newGetPatentValuationStep({
      stepType: 'CallZhihuiyaAPI',
      annotations: { groupId: 'zhihuiya', groupLabel: 'Zhihuiya', explanation: 'Read the patent valuation with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteZhihuiyaValuation'),
    }), this.complete);
  }
  /** Register the durable patent valuation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [valuationResult] }; }
}

// dex:group group-id:zhihuiya group-label:"Zhihuiya"
// dex:explanation text:"Start the patent-valuation query."
class StartPatentValuation implements Step<GetPatentValuationInput> {
  getStepType(): string { return 'StartZhihuiyaValuation'; }
  execute(_context: Context, input: GetPatentValuationInput): StepDecision { return goTo(new StepRef('CallZhihuiyaAPI'), input); }
}

// dex:group group-id:zhihuiya group-label:"Zhihuiya"
// dex:explanation text:"Persist the patent valuation and complete the Flow."
class CompletePatentValuation implements Step<QueryResult<GetPatentValuationOutput>> {
  getStepType(): string { return 'CompleteZhihuiyaValuation'; }
  execute(context: Context, result: QueryResult<GetPatentValuationOutput>): StepDecision { valuationResult.set(context, result.value); return gracefulComplete(result.value); }
}
