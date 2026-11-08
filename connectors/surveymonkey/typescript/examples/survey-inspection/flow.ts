// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListSurveysStep, type Connection, type ListSurveysInput, type SurveysPage } from '@superdurable/connector-surveymonkey';

/** Static Dex Web connection for the SurveyMonkey example. */
export const ConnectionName = 'surveymonkey';
const resultAttribute = new Attribute<SurveysPage>('surveymonkey-surveys-page', jsonCodec<SurveysPage>());

/** List surveys visible to the connected SurveyMonkey account. */
export class SurveyInspectionFlow implements Flow<ListSurveysInput> {
  private readonly complete = new CompleteSurveyInspection();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SurveyMonkey connection name differs');
    this.read = newListSurveysStep<ListSurveysInput>({
      stepType: 'CallListSurveys',
      annotations: { groupId: 'provider', groupLabel: 'SurveyMonkey', explanation: 'List surveys accessible to the connected account.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'SurveyMonkeySurveyInspection'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListSurveysInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the survey page and complete the Flow."
class CompleteSurveyInspection implements Step<QueryResult<SurveysPage>> {
  readonly inputCodec = jsonCodec<QueryResult<SurveysPage>>();
  getStepType(): string { return 'CompleteListSurveys'; }
  execute(context: Context, result: QueryResult<SurveysPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
