// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTokenUsageStep, type Connection, type GetTokenUsageInput, type GetTokenUsageOutput } from '../../src/index.js';

/** Static Dex Web connection for the IP138 example. */
export const ConnectionName = 'ip138';
const usageResult = new Attribute<GetTokenUsageOutput>('ip138-token-usage-result', jsonCodec<GetTokenUsageOutput>());

/** Read current token usage and quota through iP138. */
export class TokenUsageFlow implements Flow<GetTokenUsageInput> {
  private readonly read;
  private readonly complete = new CompleteTokenUsage();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('IP138 connection name differs');
    this.read = newGetTokenUsageStep<GetTokenUsageInput>({
      stepType: 'CallIP138',
      annotations: { groupId: 'ip138', groupLabel: 'iP138', explanation: 'Read current usage and quota for the configured token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteIP138Usage'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'IP138TokenUsage'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetTokenUsageInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable usage result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [usageResult] }; }
}

// dex:group group-id:ip138 group-label:"IP138"
// dex:explanation text:"Persist the usage counts and complete the Flow."
class CompleteTokenUsage implements Step<QueryResult<GetTokenUsageOutput>> {
  getStepType(): string { return 'CompleteIP138Usage'; }
  execute(context: Context, result: QueryResult<GetTokenUsageOutput>): StepDecision { usageResult.set(context, result.value); return gracefulComplete(result.value); }
}
