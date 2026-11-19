// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Query } from '@superdurable/connectors-sdk';
import type { MessageRole, ReasoningEffort } from '@superdurable/connectors-sdk/textgen';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { Client, type ClientOptions } from './client.js';
import { Connection as GeneratedConnection } from './generated/connection.js';
import { ConfigSchema, type Config, type Credentials, type GenerateTextRequest, type GenerateTextResponse } from './generated/models.js';
import { newGenerateTextStep as generatedGenerateTextStep } from './generated/steps.js';
import type { GenerateTextStepConfig } from './generated/steps.js';

export * from './generated/models.js';
export * from './generated/definitions.js';
export { Client } from './client.js';
export type { ClientOptions } from './client.js';

/** Named LLM connection with one generateText operation. */
export class Connection extends GeneratedConnection<{ generateText(): Query<GenerateTextRequest, GenerateTextResponse> }> {}

/** Bind a validated provider client to a static connection name. */
export function newConnection(client: Client, name: string): Connection {
  if (!client) throw new TypeError('llm client is required');
  return new Connection({ provider: 'llm', name }, {
    generateText: () => ({
      definition: () => client.generateText().definition(),
      invoke: async (call, request) => {
        const attempt = await client.generateText().invoke(call, {
        messages: (request.messages ?? []).map(message => ({ ...message, role: message.role as MessageRole })),
        ...(request.model !== undefined ? { model: request.model } : {}),
        ...(request.instructions !== undefined ? { instructions: request.instructions } : {}),
        ...(request.maxOutputTokens !== undefined ? { maxOutputTokens: request.maxOutputTokens } : {}),
        ...(request.temperature !== undefined ? { temperature: request.temperature } : {}),
        ...(request.reasoningEffort !== undefined ? { reasoningEffort: request.reasoningEffort as ReasoningEffort } : {}),
        ...(request.structuredOutput ? { structuredOutput: {
          name: request.structuredOutput.name,
          ...(request.structuredOutput.description !== undefined ? { description: request.structuredOutput.description } : {}),
          schema: request.structuredOutput.schema ?? {},
        } } : {}),
        });
        if (attempt.kind !== 'branch') return attempt;
        const value = attempt.value;
        const usage = {
          inputTokens: value.usage.inputTokens,
          outputTokens: value.usage.outputTokens,
          totalTokens: value.usage.totalTokens,
          ...(value.usage.cachedInputTokens ? { cachedInputTokens: value.usage.cachedInputTokens } : {}),
          ...(value.usage.reasoningTokens ? { reasoningTokens: value.usage.reasoningTokens } : {}),
        };
        const canonical: GenerateTextResponse = { text: value.text, usage };
        if (value.requestedModel) canonical.requestedModel = value.requestedModel;
        if (value.servedModel) canonical.servedModel = value.servedModel;
        if (value.responseId) canonical.responseId = value.responseId;
        if (value.finishReason) canonical.finishReason = value.finishReason;
        if (value.providerFinishReason) canonical.providerFinishReason = value.providerFinishReason;
        return { ...attempt, value: canonical };
      },
    }),
  });
}

/** Open a named project connection with manifest configuration and fresh credentials. */
export function newProjectConnection(project: LoadedProject, connectionName: string, options: ClientOptions = {}): Connection {
  if (!project) throw new TypeError('llm project configuration is required');
  const selected = project.configuration.connections.find(entry =>
    entry.connectorId === 'llm' && entry.connectionName === connectionName && entry.provider === 'llm');
  if (!selected) throw new TypeError('llm project connection is not configured');
  const config = ConfigSchema.parse(selected.configuration) as Config;
  const credentials: CredentialProvider<Credentials> = {
    async resolve(_call: Call, signal?: AbortSignal): Promise<Credentials> {
      const fields = await project.resolveCredential('llm', connectionName, signal);
      const decoded = z.object({ api_key: z.string() }).parse(fields);
      return { apiKey: new SecretString(decoded.api_key) };
    },
  };
  return newConnection(new Client(config, credentials, options), connectionName);
}

/** Build the manifest generateText Step from a named connection. */
export function newGenerateTextStep<StepInput>(
  configuration: GenerateTextStepConfig<StepInput, GenerateTextRequest, GenerateTextResponse>,
) {
  return generatedGenerateTextStep(configuration);
}
