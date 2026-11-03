// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryMetricsStep, type Connection, type MetricsQuery, type QueryMetricsInput } from '../../src/index.js';

/** Static Datadog connection name configured in Dex Web. */
export const ConnectionName = 'datadog';
/** Safe timeseries result retained by the Flow. */
export const metricsResult = new Attribute<MetricsQuery>('datadog-metrics-result', jsonCodec<MetricsQuery>());

/** Query one Datadog metric window and complete with its timeseries. */
export class MetricsLookupFlow implements Flow<QueryMetricsInput> {
  private readonly prepare = new PrepareMetricsLookup();
  private readonly complete = new CompleteMetricsLookup();

  /** Bind the static named connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('metrics lookup requires the static Datadog connection');
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'DatadogMetricsLookup'; }
  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<QueryMetricsInput> {
    return StepList.startStep<QueryMetricsInput>(this.prepare).otherSteps(
      newQueryMetricsStep({
        stepType: 'QueryDatadogMetrics',
        annotations: { groupId: 'datadog-metrics', groupLabel: 'Datadog Metrics', explanation: 'Query Datadog timeseries points for the requested window.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input, queried: this.complete,
      }), this.complete,
    );
  }
  /** Register the persisted result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [metricsResult] }; }
}

// dex:group group-id:datadog-metrics group-label:"Datadog Metrics"
// dex:explanation text:"Pass the requested Unix-second metric window to the Datadog Query."
class PrepareMetricsLookup implements Step<QueryMetricsInput> {
  getStepType(): string { return 'PrepareDatadogMetricsLookup'; }
  execute(_context: Context, input: QueryMetricsInput): StepDecision { return goTo(new StepRef('QueryDatadogMetrics'), input); }
}

// dex:group group-id:datadog-metrics group-label:"Datadog Metrics"
// dex:explanation text:"Retain the returned timeseries and complete the lookup."
class CompleteMetricsLookup implements Step<QueryResult<MetricsQuery>> {
  getStepType(): string { return 'CompleteDatadogMetricsLookup'; }
  execute(context: Context, result: QueryResult<MetricsQuery>): StepDecision {
    metricsResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
