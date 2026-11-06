// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, goTo, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendRequestStep, type Connection, type SendRequestOutput } from '@superdurable/connector-superdurable-oauth2-http';

/** Stable Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'oauth2-http-api';

/** Safe input for one authenticated GET request. */
export interface RequestInput { path: string; }

/** Request an API path and return its bounded answer. */
export class APIRequestFlow implements Flow<RequestInput> {
  private readonly prepare = new PrepareRequest();
  private readonly complete = new CompleteRequest();

  /** Bind the static provider connection at Worker registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('OAuth 2.0 HTTP connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'OAuth2HTTPRequest'; }

  /** Register the preparation, connector, and completion Steps. */
  getSteps(): StepList<RequestInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSendRequestStep({ stepType: 'SendRequest', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'oauth2-http', groupLabel: 'OAuth 2.0 HTTP', explanation: 'Send a GET request to the configured API using its renewable access token.' },
        mapToOperationInput: input => ({ method: 'GET', path: input.path }), completed: this.complete }),
      this.complete,
    );
  }
}

/** Validate the requested API path. */
// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Check the requested API path before the authenticated provider Step."
export class PrepareRequest implements Step<RequestInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareOAuth2HTTPRequest'; }
  /** Continue to the connector Step. */
  execute(_context: Context, input: RequestInput): StepDecision {
    return goTo(new StepRef('SendRequest'), input);
  }
}

/** Complete with the bounded provider answer. */
// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Return the API answer after the connector completes."
export class CompleteRequest implements Step<MutationResult<SendRequestOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteOAuth2HTTPRequest'; }
  /** Return the connector result. */
  execute(_context: Context, result: MutationResult<SendRequestOutput>): StepDecision { return gracefulComplete(result.value); }
}
