// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newGetCandidateStep, type Connection, type CandidateProfile, type GetCandidateInput,
} from '@superdurable/connector-greenhouse';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'greenhouse';
const candidateAttribute = new Attribute<CandidateProfile>('greenhouse-candidate', jsonCodec<CandidateProfile>());

/** Read one candidate through a typed Greenhouse Query. */
export class CandidateInspectionFlow implements Flow<GetCandidateInput> {
  private readonly complete = new CompleteCandidateInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Greenhouse connection name differs');
    this.read = newGetCandidateStep<GetCandidateInput>({
      stepType: 'CallGetCandidate',
      annotations: { groupId: 'provider', groupLabel: 'Greenhouse', explanation: 'Read one candidate from Greenhouse.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGreenhouse'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'GreenhouseCandidateInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<GetCandidateInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected candidate result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [candidateAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected candidate and complete the Flow."
class CompleteCandidateInspection implements Step<QueryResult<CandidateProfile>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGreenhouse'; }
  /** Persist and return the selected candidate. */
  execute(context: Context, result: QueryResult<CandidateProfile>): StepDecision {
    candidateAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
