// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetStatsStep, type Connection, type GetStatsInput, type StatsOutput } from '@superdurable/connector-sendgrid';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'sendgrid';
/** Safe request fields retained for Flow inspection. */
export const statsRequest = new Attribute<GetStatsInput>('sendgrid-stats-request', jsonCodec<GetStatsInput>());
/** Safe provider result retained for Flow inspection. */
export const statsResult = new Attribute<StatsOutput>('sendgrid-stats-result', jsonCodec<StatsOutput>());

/** One SendGrid statistics Flow. */
export class GetStatsFlow implements Flow<GetStatsInput> {
  private readonly prepare = new PrepareGetStats();
  private readonly complete = new CompleteGetStats();
  /** Bind the named SendGrid connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('SendGrid connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'SendGridGetStatsFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<GetStatsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetStatsStep({
        stepType: 'ReadSendGridStats',
        annotations: { groupId: 'sendgrid-stats', groupLabel: 'SendGrid Statistics', explanation: 'Read global email statistics for an inclusive date range.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted request and result Attributes. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [statsRequest, statsResult] }; }
}

/** Retain safe request fields before the provider call. */
// dex:group group-id:sendgrid-stats group-label:"SendGrid Statistics"
// dex:explanation text:"Retain the requested date range before querying SendGrid."
export class PrepareGetStats implements Step<GetStatsInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareSendGridStats'; }
  /** Route to the provider Step. */
  execute(context: Context, input: GetStatsInput): StepDecision {
    statsRequest.set(context, input);
    return goTo(new StepRef('ReadSendGridStats'), input);
  }
}

/** Persist and return a successful statistics result. */
// dex:group group-id:sendgrid-stats group-label:"SendGrid Statistics"
// dex:explanation text:"Persist the returned daily counts and complete the Flow."
export class CompleteGetStats implements Step<QueryResult<StatsOutput>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteSendGridStats'; }
  /** Return the provider result. */
  execute(context: Context, result: QueryResult<StatsOutput>): StepDecision {
    statsResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
