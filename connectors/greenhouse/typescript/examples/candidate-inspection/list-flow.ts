// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListCandidatesStep, type Connection, type CandidateProfilesPage, type ListCandidatesInput,
} from '@superdurable/connector-greenhouse';

import { ConnectionName } from './flow.js';
const candidateAttribute = new Attribute<CandidateProfilesPage>('greenhouse-candidate-page', jsonCodec<CandidateProfilesPage>());

/** Read one page of candidate profiles through a typed Greenhouse Query. */
export class CandidateListFlow implements Flow<ListCandidatesInput> {
  private readonly complete = new CompleteCandidateList();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Greenhouse connection name differs');
    this.read = newListCandidatesStep<ListCandidatesInput>({
      stepType: 'CallListCandidates',
      annotations: { groupId: 'provider', groupLabel: 'Greenhouse', explanation: 'Read one page of candidate profiles from Greenhouse.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteGreenhouseList'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'GreenhouseCandidateListTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListCandidatesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the selected candidate result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [candidateAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected candidate and complete the Flow."
class CompleteCandidateList implements Step<QueryResult<CandidateProfilesPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGreenhouseList'; }
  /** Persist and return the selected candidate. */
  execute(context: Context, result: QueryResult<CandidateProfilesPage>): StepDecision {
    candidateAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
