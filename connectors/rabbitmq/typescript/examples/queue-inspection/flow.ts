// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetQueueMetricsStep, type Connection, type QueueMetrics, type GetQueueMetricsInput,
} from '@superdurable/connector-rabbitmq';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'rabbitmq';
const metricsAttribute = new Attribute<QueueMetrics>('rabbitmq-queue-inspection', jsonCodec<QueueMetrics>());

/** Read current counts through a typed RabbitMQ Query. */
export class QueueInspectionFlow implements Flow<GetQueueMetricsInput> {
  private readonly complete = new CompleteQueueInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('RabbitMQ connection name differs');
    this.read = newGetQueueMetricsStep<GetQueueMetricsInput>({
      stepType: 'CallGetQueueMetrics',
      annotations: { groupId: 'provider', groupLabel: 'RabbitMQ', explanation: 'Read current counts for one RabbitMQ queue.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteRabbitMQ'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'RabbitMQQueueInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetQueueMetricsInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the queue metrics. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [metricsAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the queue metrics and complete the Flow."
class CompleteQueueInspection implements Step<QueryResult<QueueMetrics>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteRabbitMQ'; }
  /** Persist and return the current queue metrics. */
  execute(context: Context, result: QueryResult<QueueMetrics>): StepDecision {
    metricsAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
