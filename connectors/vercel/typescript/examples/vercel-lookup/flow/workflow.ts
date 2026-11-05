// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetDeploymentStep, type Connection, type Deployment, type GetDeploymentInput } from '@superdurable/connector-vercel';

/** Static Dex Web connection name used by this application. */
export const ConnectionName = 'vercel';
/** Nonsecret lookup request retained for inspection. */
export const lookupRequest = new Attribute<GetDeploymentInput>('vercel-lookup-request', jsonCodec<GetDeploymentInput>());
/** Deployment details retained for inspection. */
export const deploymentResult = new Attribute<Deployment>('vercel-deployment-result', jsonCodec<Deployment>());
/** Read a Vercel deployment by ID or hostname. */
export class VercelLookupFlow implements Flow<GetDeploymentInput> {
  private readonly prepare = new PrepareLookup();
  private readonly complete = new CompleteLookup();
  /** Bind the named connection at Worker registration. */ constructor(private readonly connection: Connection) {}
  /** Stable Dex Flow type. */ getFlowType(): string { return 'VercelLookupFlow'; }
  /** Register lookup Steps. */ getSteps(): StepList<GetDeploymentInput> {
    return StepList.startStep(this.prepare).otherSteps(newGetDeploymentStep({
      stepType: 'ReadVercelDeployment', annotations: { groupId: 'vercel-lookup', groupLabel: 'Vercel Lookup', explanation: 'Read one Vercel deployment by ID or hostname.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.complete,
    }), this.complete);
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [lookupRequest, deploymentResult] }; }
}
// dex:group group-id:vercel-lookup group-label:"Vercel Lookup"
// dex:explanation text:"Validate the deployment identifier before calling Vercel."
/** Validate the nonsecret lookup input. */
export class PrepareLookup implements Step<GetDeploymentInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareVercelLookup'; }
  /** Start the provider Step. */ execute(context: Context, input: GetDeploymentInput): StepDecision {
    if (!input.idOrUrl?.trim()) return forceFail('A Vercel deployment ID or hostname is required');
    lookupRequest.set(context, input);
    return goTo(new StepRef('ReadVercelDeployment'), input);
  }
}
// dex:group group-id:vercel-lookup group-label:"Vercel Lookup"
// dex:explanation text:"Persist the deployment summary and complete the Flow."
/** Complete a successful lookup. */
export class CompleteLookup implements Step<QueryResult<Deployment>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteVercelLookup'; }
  /** Persist the provider result. */ execute(context: Context, result: QueryResult<Deployment>): StepDecision {
    deploymentResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
