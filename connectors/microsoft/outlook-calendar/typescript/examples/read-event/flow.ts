// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StepList,
  forceComplete,
  type Context,
  type Flow,
  type Step,
  type StepDecision,
} from "@superdurable/dex";
import type { QueryResult } from "@superdurable/connectors-sdk";
import {
  newGetEventStep,
  type Connection,
  type Event,
  type GetEventInput,
} from "@superdurable/connector-microsoft-outlook-calendar";

/** Static Dex Web connection name for the Outlook Calendar example. */
export const ConnectionName = "outlook-calendar-scheduler";

/** Read one Outlook Calendar event through a real Dex Worker. */
export class ReadEventFlow implements Flow<GetEventInput> {
  private readonly doneStep = new ReadCompletedStep();
  private readonly readStep;

  /** Bind the statically named connection. */
  constructor(connection: Connection) {
    this.readStep = newGetEventStep<GetEventInput>({
      stepType: "ReadOutlookCalendarEvent",
      connectionName: ConnectionName,
      connection,
      annotations: {
        groupId: "outlook-calendar",
        groupLabel: "Outlook Calendar",
        explanation: "Read the selected Outlook calendar event.",
      },
      mapToOperationInput: (input) => input,
      found: this.doneStep,
    });
  }

  /** Stable Flow identity. */
  getFlowType(): string {
    return "OutlookCalendarReadEventExample";
  }
  /** Register the provider Step and terminal receiver. */
  getSteps() {
    return StepList.startStep(this.readStep).otherSteps(this.doneStep);
  }
}

// dex:group group-id:outlook-calendar group-label:"Outlook Calendar"
// dex:explanation text:"Complete after the Outlook calendar event is read."
class ReadCompletedStep implements Step<QueryResult<Event>> {
  getStepType(): string {
    return "CompleteOutlookCalendarRead";
  }
  execute(context: Context, result: QueryResult<Event>): StepDecision {
    void context;
    return forceComplete(result);
  }
}
