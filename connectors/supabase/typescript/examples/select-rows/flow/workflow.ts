// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSelectRowsStep, type Connection, type SelectRowsInput, type SelectRowsOutput } from '@superdurable/connector-supabase';

/** Static Dex Web connection name used by this Worker and starter. */
export const ConnectionName = 'supabase';
/** Safe selected rows retained by the Flow. */
export const selectedRows = new Attribute<SelectRowsOutput>('supabase-selected-rows', jsonCodec<SelectRowsOutput>());

/** Query one bounded Supabase table page. */
export class SelectRowsFlow implements Flow<SelectRowsInput> {
  private readonly prepare = new PrepareSelectRows();
  private readonly complete = new CompleteSelectRows();
  /** Bind the named connection at Worker registration. */ constructor(private readonly connection: Connection) {}
  /** Stable Dex Flow type. */ getFlowType(): string { return 'SupabaseSelectRowsFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<SelectRowsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSelectRowsStep({ stepType: 'SelectSupabaseRows',
        annotations: { groupId: 'supabase-select', groupLabel: 'Supabase Rows', explanation: 'Select one bounded page of Supabase table rows.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input, selected: this.complete }),
      this.complete);
  }
  /** Register persisted results. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [selectedRows] }; }
}

/** Pass safe table selection to the provider Step. */
// dex:group group-id:supabase-select group-label:"Supabase Rows"
// dex:explanation text:"Pass a bounded table selection to the provider Step."
export class PrepareSelectRows implements Step<SelectRowsInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareSupabaseSelectRows'; }
  /** Begin the provider Step. */ execute(_context: Context, input: SelectRowsInput): StepDecision { return goTo(new StepRef('SelectSupabaseRows'), input); }
}

/** Persist the safe result and complete. */
// dex:group group-id:supabase-select group-label:"Supabase Rows"
// dex:explanation text:"Persist the selected rows and complete the Flow."
export class CompleteSelectRows implements Step<QueryResult<SelectRowsOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteSupabaseSelectRows'; }
  /** Save the selected data. */ execute(context: Context, result: QueryResult<SelectRowsOutput>): StepDecision {
    selectedRows.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
