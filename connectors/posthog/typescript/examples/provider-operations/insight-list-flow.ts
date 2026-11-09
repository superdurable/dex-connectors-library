// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListInsightsStep, type Connection, type InsightsPage, type ListInsightsInput } from '@superdurable/connector-posthog';

export const ConnectionName = 'posthog';
const insightsAttribute = new Attribute<InsightsPage>('posthog-typescript-insights-page', jsonCodec<InsightsPage>());

/** Read one bounded page of saved insight IDs and names. */
export class InsightListFlow implements Flow<ListInsightsInput> {
  private readonly complete = new CompleteInsights();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('PostHog connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PostHogTypeScriptInsightList'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListInsightsInput> {
    return StepList.startStep(newListInsightsStep<ListInsightsInput>({
      stepType: 'CallPostHogListInsights',
      annotations: { groupId: 'insight-list', groupLabel: 'Insight list',
        explanation: 'List a bounded page of saved PostHog insight identities.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted insight page. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [insightsAttribute] };
  }
}

// dex:group group-id:insight-list group-label:"Insight list"
// dex:explanation text:"Persist the bounded saved insight page and complete the Flow."
export class CompleteInsights implements Step<QueryResult<InsightsPage>> {
  readonly inputCodec = jsonCodec<QueryResult<InsightsPage>>();
  getStepType(): string { return 'CompletePostHogInsights'; }
  execute(context: Context, result: QueryResult<InsightsPage>): StepDecision {
    insightsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
