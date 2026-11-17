// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDescribeRegionsStep, type Connection, type DescribeRegionsInput, type DescribeRegionsOutput } from '../../src/index.js';

/** Static Dex Web connection for the Alibaba Cloud example. */
export const ConnectionName = 'alibaba';
const regionsResult = new Attribute<DescribeRegionsOutput>('alibaba-ecs-regions-result', jsonCodec<DescribeRegionsOutput>());

/** Read the ECS region list through Alibaba Cloud OpenAPI. */
export class ECSRegionsFlow implements Flow<DescribeRegionsInput> {
  private readonly start = new StartRegions();
  private readonly complete = new CompleteRegions();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Alibaba Cloud connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'AlibabaECSRegions'; }
  /** Register the signed provider and terminal Steps. */
  getSteps(): StepList<DescribeRegionsInput> {
    return StepList.startStep(this.start).otherSteps(newDescribeRegionsStep({
      stepType: 'CallAlibabaECS',
      annotations: { groupId: 'alibaba', groupLabel: 'Alibaba Cloud', explanation: 'Read the ECS region catalog with a signed OpenAPI query.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, fetched: new StepRef('CompleteAlibabaRegions'),
    }), this.complete);
  }
  /** Register the durable region catalog. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [regionsResult] }; }
}

// dex:group group-id:alibaba group-label:"Alibaba Cloud"
// dex:explanation text:"Start the signed ECS region query."
class StartRegions implements Step<DescribeRegionsInput> {
  getStepType(): string { return 'StartAlibabaRegions'; }
  execute(_context: Context, input: DescribeRegionsInput): StepDecision { return goTo(new StepRef('CallAlibabaECS'), input); }
}

// dex:group group-id:alibaba group-label:"Alibaba Cloud"
// dex:explanation text:"Persist the region catalog and complete the Flow."
class CompleteRegions implements Step<QueryResult<DescribeRegionsOutput>> {
  getStepType(): string { return 'CompleteAlibabaRegions'; }
  execute(context: Context, result: QueryResult<DescribeRegionsOutput>): StepDecision { regionsResult.set(context, result.value); return gracefulComplete(result.value); }
}
