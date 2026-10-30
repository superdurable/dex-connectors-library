// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepRef, forceFail, goTo, retryAfter, type AsyncContext, type Attribute, type BufferedTextStreamOptions, type Codec, type Step, type StepDecision, type StepOptions, type StepTarget, type Stream } from '@superdurable/dex';
import { createCall, type ProgressUpdate } from './credentials.js';
import { RetryError, type Failure } from './errors.js';
import { validateMutationDefinition, validateQueryDefinition, type Mutation, type Query, type StepDefaults } from './operation.js';
import { runMutation, runQuery } from './run_operation.js';
import { ConnectionRefSchema, ConnectorConfigurationUISchema, defaultConnectorStepExecuteRetryPolicy, type BranchDefinition, type ConnectionRef, type ConnectorConfigurationUI, type MutationResult, type QueryResult } from './types.js';

/** Stable Dex authoring metadata for one Connector Step. */
export interface StepAnnotations {
  /** Lowercase stable group identifier. */ groupId: string;
  /** Human-readable group label. */ groupLabel: string;
  /** One-sentence Step explanation. */ explanation: string;
}

/** Typed branch destination, including a registered Step instance or StepRef. */
export interface BranchTarget<Result> {
  /** Declared operation branch. */ branch: string;
  /** Destination; an omitted target is allowed only for an optional branch. */ target?: StepTarget<Result>;
}

/** Bind one declared branch to a registered Step destination. */
export function goToBranch<Result>(branch: string, target?: StepTarget<Result>): BranchTarget<Result> { return target ? { branch, target } : { branch }; }

/** Address a registered Step by stable type without constructing its implementation. */
export function stepRef(stepType: string): StepRef { return new StepRef(stepType); }

/** Configure an advanced generic Query Step; operation-specific generated factories are preferred. */
export interface QueryStepConfig<StepInput, OperationInput, Output> {
  /** Stable Dex Step type. */ stepType: string;
  /** Group and explanation metadata. */ annotations: StepAnnotations;
  /** Optional Connector Studio configuration UI. */ configurationUI?: ConnectorConfigurationUI;
  /** Read-only provider operation. */ operation: Query<OperationInput, Output>;
  /** Logical credential selection. */ connection: ConnectionRef;
  /** Map the application's Step input to operation input. */ mapToOperationInput(input: StepInput): OperationInput;
  /** Routes for every required branch and selected optional branches. */ branches: readonly BranchTarget<QueryResult<Output>>[];
  /** Optional terminal result Attribute registered in the Flow schema. */ resultAttribute?: Attribute<QueryResult<Output>>;
  /** Optional structured progress Stream registered in the Flow schema. */ progressStream?: Stream<ProgressUpdate>;
  /** Optional buffered text Stream registered in the Flow schema. */ textStream?: Stream<string>;
  /** Optional text batch sizing and flush interval. */ textOptions?: BufferedTextStreamOptions;
  /** Optional codec for non-object Step input. */ inputCodec?: Codec<StepInput>;
  /** Execute-only Dex options overriding manifest defaults. */ stepOptionsOverride?: StepOptions;
}

/** Configure an advanced generic Mutation Step. */
export interface MutationStepConfig<StepInput, OperationInput, Output> extends Omit<QueryStepConfig<StepInput, OperationInput, Output>, 'operation' | 'branches' | 'resultAttribute'> {
  /** Provider write operation. */ operation: Mutation<OperationInput, Output>;
  /** Routes for every required branch and selected optional branches. */ branches: readonly BranchTarget<MutationResult<Output>>[];
  /** Optional terminal result Attribute registered in the Flow schema. */ resultAttribute?: Attribute<MutationResult<Output>>;
}

interface StepIdentity { operation: { connectorId: string; operationId: string }; connection: ConnectionRef }

/** Query Step that invokes one provider operation per Dex Execute attempt. */
export class QueryStep<StepInput, OperationInput, Output> implements Step<StepInput> {
  readonly inputCodec?: Codec<StepInput>;
  readonly #configuration: QueryStepConfig<StepInput, OperationInput, Output>;
  readonly #options: StepOptions;
  readonly #ui: ConnectorConfigurationUI;
  readonly #targets: ReadonlyMap<string, StepTarget<QueryResult<Output>>>;
  readonly #optional: ReadonlySet<string>;
  readonly #identity: StepIdentity;

  /** Validate static wiring before registering the Step in a Flow. */
  constructor(configuration: QueryStepConfig<StepInput, OperationInput, Output>) {
    if (!configuration?.operation) throw new TypeError('query operation is required');
    const definition = configuration.operation.definition();
    validateQueryDefinition(definition);
    validateFactoryConfiguration(configuration);
    const routes = validateBranchTargets(definition.branches, configuration.branches);
    this.#configuration = configuration;
    this.#options = mergeStepOptions(definition.stepDefaults, configuration.stepOptionsOverride);
    this.#ui = ConnectorConfigurationUISchema.parse(configuration.configurationUI ?? { units: [] });
    this.#targets = routes.targets;
    this.#optional = routes.optional;
    this.#identity = { operation: definition.operation, connection: configuration.connection };
    if (configuration.inputCodec) this.inputCodec = configuration.inputCodec;
  }

  /** Stable Dex Step type. */ getStepType(): string { return this.#configuration.stepType; }
  /** Isolated Execute options, including connector retry defaults. */ getStepOptions(): StepOptions { return cloneStepOptions(this.#options); }
  /** Group metadata for Connector Studio. */ annotations(): StepAnnotations { return { ...this.#configuration.annotations }; }
  /** Isolated connector UI composition. */ configurationUI(): ConnectorConfigurationUI { return structuredClone(this.#ui); }

  /** Invoke, persist the optional terminal Attribute, and route the selected branch. */
  async execute(context: AsyncContext, input: StepInput): Promise<StepDecision> {
    const operationInput = this.#configuration.mapToOperationInput(input);
    const call = createCall(context, this.#identity.connection, this.#identity.operation, {
      ...(this.#configuration.progressStream ? { progressStream: this.#configuration.progressStream } : {}),
      ...(this.#configuration.textStream ? { textStream: this.#configuration.textStream } : {}),
      ...(this.#configuration.textOptions ? { textOptions: this.#configuration.textOptions } : {}),
    });
    let result: QueryResult<Output>;
    try { result = await runQuery(this.#configuration.operation, call, operationInput); }
    catch (error) { throwDexRetry(error); }
    if (this.#configuration.resultAttribute) context.setAttribute(this.#configuration.resultAttribute, result);
    return routeBranch(this.#targets, this.#optional, result, this.#identity, 'query');
  }
}

/** Mutation Step that keeps stable provider identity across Dex retries. */
export class MutationStep<StepInput, OperationInput, Output> implements Step<StepInput> {
  readonly inputCodec?: Codec<StepInput>;
  readonly #configuration: MutationStepConfig<StepInput, OperationInput, Output>;
  readonly #options: StepOptions;
  readonly #ui: ConnectorConfigurationUI;
  readonly #targets: ReadonlyMap<string, StepTarget<MutationResult<Output>>>;
  readonly #optional: ReadonlySet<string>;
  readonly #identity: StepIdentity;

  /** Validate static wiring before registering the Step in a Flow. */
  constructor(configuration: MutationStepConfig<StepInput, OperationInput, Output>) {
    if (!configuration?.operation) throw new TypeError('mutation operation is required');
    const definition = configuration.operation.definition();
    validateMutationDefinition(definition);
    validateFactoryConfiguration(configuration);
    const routes = validateBranchTargets(definition.branches, configuration.branches);
    this.#configuration = configuration;
    this.#options = mergeStepOptions(definition.stepDefaults, configuration.stepOptionsOverride);
    this.#ui = ConnectorConfigurationUISchema.parse(configuration.configurationUI ?? { units: [] });
    this.#targets = routes.targets;
    this.#optional = routes.optional;
    this.#identity = { operation: definition.operation, connection: configuration.connection };
    if (configuration.inputCodec) this.inputCodec = configuration.inputCodec;
  }

  /** Stable Dex Step type. */ getStepType(): string { return this.#configuration.stepType; }
  /** Isolated Execute options, including connector retry defaults. */ getStepOptions(): StepOptions { return cloneStepOptions(this.#options); }
  /** Group metadata for Connector Studio. */ annotations(): StepAnnotations { return { ...this.#configuration.annotations }; }
  /** Isolated connector UI composition. */ configurationUI(): ConnectorConfigurationUI { return structuredClone(this.#ui); }

  /** Invoke, persist the optional terminal Attribute, and route the selected branch. */
  async execute(context: AsyncContext, input: StepInput): Promise<StepDecision> {
    const operationInput = this.#configuration.mapToOperationInput(input);
    const call = createCall(context, this.#identity.connection, this.#identity.operation, {
      ...(this.#configuration.progressStream ? { progressStream: this.#configuration.progressStream } : {}),
      ...(this.#configuration.textStream ? { textStream: this.#configuration.textStream } : {}),
      ...(this.#configuration.textOptions ? { textOptions: this.#configuration.textOptions } : {}),
    });
    let result: MutationResult<Output>;
    try { result = await runMutation(this.#configuration.operation, call, operationInput); }
    catch (error) { throwDexRetry(error); }
    if (this.#configuration.resultAttribute) context.setAttribute(this.#configuration.resultAttribute, result);
    return routeBranch(this.#targets, this.#optional, result, this.#identity, 'mutation');
  }
}

/** Construct a Query Step after validating its static routing. */
export function newQueryStep<StepInput, OperationInput, Output>(configuration: QueryStepConfig<StepInput, OperationInput, Output>): QueryStep<StepInput, OperationInput, Output> { return new QueryStep(configuration); }
/** Construct a Mutation Step after validating its static routing. */
export function newMutationStep<StepInput, OperationInput, Output>(configuration: MutationStepConfig<StepInput, OperationInput, Output>): MutationStep<StepInput, OperationInput, Output> { return new MutationStep(configuration); }

function validateFactoryConfiguration(configuration: Pick<QueryStepConfig<unknown, unknown, unknown>, 'stepType' | 'annotations' | 'connection' | 'mapToOperationInput'>): void {
  if (!configuration.stepType?.trim()) throw new TypeError('stable Step type is required');
  if (!/^[a-z][a-z0-9-]*$/.test(configuration.annotations?.groupId ?? '') || !configuration.annotations.groupLabel?.trim() || !configuration.annotations.explanation?.trim()) throw new TypeError('Step group, group label, and explanation are required');
  ConnectionRefSchema.parse(configuration.connection);
  if (typeof configuration.mapToOperationInput !== 'function') throw new TypeError('mapToOperationInput is required');
}

function validateBranchTargets<Result>(definitions: readonly BranchDefinition[], routes: readonly BranchTarget<Result>[]): { targets: ReadonlyMap<string, StepTarget<Result>>; optional: ReadonlySet<string> } {
  const expected = new Map(definitions.map(branch => [branch.id, branch]));
  const targets = new Map<string, StepTarget<Result>>();
  const optional = new Set(definitions.filter(branch => branch.optional).map(branch => branch.id));
  for (const route of routes) {
    if (!expected.has(route.branch)) throw new TypeError(`branch target ${route.branch} is not declared by the operation`);
    if (!route.target) {
      if (!optional.has(route.branch)) throw new TypeError(`branch target ${route.branch} must name a Step`);
      continue;
    }
    if (route.target instanceof StepRef && !route.target.stepType.trim()) throw new TypeError(`branch target ${route.branch} must name a Step`);
    if (typeof route.target === 'object' && !(route.target instanceof StepRef) && !route.target.getStepType().trim()) throw new TypeError(`branch target ${route.branch} must name a Step`);
    if (targets.has(route.branch)) throw new TypeError(`branch target ${route.branch} is duplicated`);
    targets.set(route.branch, route.target);
  }
  for (const branch of definitions) if (!branch.optional && !targets.has(branch.id)) throw new TypeError(`branch target ${branch.id} is required`);
  return { targets, optional };
}

function mergeStepOptions(defaults: StepDefaults, override?: StepOptions): StepOptions {
  if (override && Object.keys(override).some(key => key.startsWith('waitFor') && override[key as keyof StepOptions] !== undefined)) throw new TypeError('execute-only connector Step rejects WaitFor options');
  const merged: StepOptions = {
    ...((override?.executeMethodTimeoutMs || defaults.executeMethodTimeoutMs) ? { executeMethodTimeoutMs: override?.executeMethodTimeoutMs || defaults.executeMethodTimeoutMs } : {}),
    ...((override?.heartbeatTimeoutMs || defaults.heartbeatTimeoutMs) ? { heartbeatTimeoutMs: override?.heartbeatTimeoutMs || defaults.heartbeatTimeoutMs } : {}),
    executeRetry: { ...(override?.executeRetry ?? defaults.executeRetry ?? defaultConnectorStepExecuteRetryPolicy()) },
    ...((override?.executeDurability || (defaults.executeDurability !== 'default' ? defaults.executeDurability : undefined)) ? { executeDurability: override?.executeDurability ?? defaults.executeDurability as 'sync' | 'async' } : {}),
    ...(override?.executeFailure ? { executeFailure: override.executeFailure } : {}),
    ...(override?.executeLockAttributes ? { executeLockAttributes: override.executeLockAttributes } : {}),
    ...(override?.executeLoadAttributeMaps ? { executeLoadAttributeMaps: override.executeLoadAttributeMaps } : {}),
    ...(override?.executeLoadAttributeMapInstances ? { executeLoadAttributeMapInstances: override.executeLoadAttributeMapInstances } : {}),
    ...(override?.executeLoadChannels ? { executeLoadChannels: override.executeLoadChannels } : {}),
    ...(override?.executeLoadChannelMaps ? { executeLoadChannelMaps: override.executeLoadChannelMaps } : {}),
    ...(override?.executeLoadChannelMapInstances ? { executeLoadChannelMapInstances: override.executeLoadChannelMapInstances } : {}),
  };
  return cloneStepOptions(merged);
}

function cloneStepOptions(options: StepOptions): StepOptions {
  return { ...options, ...(options.executeRetry ? { executeRetry: { ...options.executeRetry } } : {}),
    ...(options.executeLockAttributes ? { executeLockAttributes: [...options.executeLockAttributes] } : {}),
    ...(options.executeLoadAttributeMaps ? { executeLoadAttributeMaps: [...options.executeLoadAttributeMaps] } : {}),
    ...(options.executeLoadAttributeMapInstances ? { executeLoadAttributeMapInstances: [...options.executeLoadAttributeMapInstances] } : {}),
    ...(options.executeLoadChannels ? { executeLoadChannels: [...options.executeLoadChannels] } : {}),
    ...(options.executeLoadChannelMaps ? { executeLoadChannelMaps: [...options.executeLoadChannelMaps] } : {}),
    ...(options.executeLoadChannelMapInstances ? { executeLoadChannelMapInstances: [...options.executeLoadChannelMapInstances] } : {}) };
}

function routeBranch<Result extends { branch: string; failure?: Failure }>(targets: ReadonlyMap<string, StepTarget<Result>>, optional: ReadonlySet<string>, result: Result, identity: StepIdentity, operationKind: string): StepDecision {
  const target = targets.get(result.branch);
  if (target) return goTo(target, result);
  if (optional.has(result.branch)) {
    let reason = `${identity.operation.connectorId} ${identity.operation.operationId} (connection ${identity.connection.name}) selected branch ${result.branch} with no target`;
    if (result.failure) reason += `: ${result.failure.kind}: ${boundedFailureMessage(result.failure.message)}`;
    return forceFail(reason);
  }
  throw new TypeError(`${operationKind} result selected unconfigured branch ${result.branch}`);
}

function boundedFailureMessage(message: string): string {
  const bytes = Buffer.from(message);
  if (bytes.length <= 1024) return message;
  let end = 1024;
  while (end > 0 && (bytes[end]! & 0xc0) === 0x80) end--;
  return `${bytes.subarray(0, end).toString('utf8')} (truncated)`;
}

function throwDexRetry(error: unknown): never {
  if (error instanceof RetryError && error.retryAfterMs >= 1000) throw retryAfter(Math.floor(error.retryAfterMs / 1000), error);
  throw error;
}
