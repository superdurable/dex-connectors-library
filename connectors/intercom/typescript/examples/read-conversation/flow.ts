// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StepList,
  gracefulComplete,
  type Context,
  type Flow,
  type PersistenceSchema,
  type Step,
  type StepDecision,
} from "@superdurable/dex";
import type { QueryResult } from "@superdurable/connectors-sdk";
import {
  newGetConversationStep,
  type Connection,
  type ConversationDetails,
  type GetConversationInput,
} from "../../src/index.js";

/** Static Dex Web connection name for this Flow. */
export const ConnectionName = "intercom";

/** Read one conversation through the generated operation factory. */
export class ReadConversationFlow implements Flow<GetConversationInput> {
  private readonly found = new ConversationFound();
  private readonly readConversation;
  /** Bind one static project or scripted connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName)
      throw new TypeError(
        "read-conversation requires its static Intercom connection",
      );
    this.readConversation = newGetConversationStep<GetConversationInput>({
      stepType: "ReadIntercomConversation",
      annotations: {
        groupId: "intercom",
        groupLabel: "Intercom",
        explanation:
          "Read the conversation and its recent parts as plain text.",
      },
      connection,
      connectionName: ConnectionName,
      mapToOperationInput: (input) => input,
      found: this.found,
    });
  }
  /** Return the stable Dex Flow identity. */ getFlowType(): string {
    return "IntercomReadConversation";
  }
  /** Register the read and completion Steps. */ getSteps(): StepList<GetConversationInput> {
    return StepList.startStep(this.readConversation).otherSteps(this.found);
  }
  /** This Flow persists no application Attributes. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [] };
  }
}

// dex:group group-id:intercom group-label:"Intercom"
// dex:explanation text:"Complete with the conversation and latest parts returned by Intercom."
class ConversationFound implements Step<QueryResult<ConversationDetails>> {
  getStepType(): string {
    return "IntercomConversationFound";
  }
  execute(
    _context: Context,
    result: QueryResult<ConversationDetails>,
  ): StepDecision {
    return gracefulComplete(result.value);
  }
}
