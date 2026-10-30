// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call } from '../credentials.js';
import { FailureSchema, type Failure } from '../errors.js';
import { validateMutationDefinition, validateQueryDefinition, type Mutation as MutationOperation, type MutationDefinition, type Query as QueryOperation, type QueryDefinition } from '../operation.js';
import { DefectBranchID, UncertainBranchID, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type BranchID, type MutationAttempt, type QueryAttempt } from '../types.js';
import type { Case, RecordedCall } from './index.js';

/** One answered mock call, including input only in test memory. */
export interface MockCall<Input> extends RecordedCall {
  /** Provider-safe idempotency key selected by the SDK. */ idempotencyKey?: string;
  /** Input observed by the mock. */ input: Input;
}

/** Constructor-injected checks for a scripted connector operation. */
export interface MockOptions<Input, Output> {
  /** Return a safe failure to select defect before case selection. */ requestCheck?: (input: Input) => Failure | undefined;
  /** Validate a selected success branch against the request. */ answerCheck?: { branch: BranchID; check: (input: Input, output: Output) => void };
  /** Receive scripting mistakes and unscripted-call reports. */ onFailure?: (message: string) => void;
}

interface Rule<Input, Output> { matches: (input: Input) => boolean; response: Case<Output> }

class Script<Input, Output> {
  readonly rules: Rule<Input, Output>[] = [];
  readonly queue: Case<Output>[] = [];
  defaults: Case<Output>[] = [];
  defaultCount = 0;

  nextScripted(input: Input): Case<Output> | undefined {
    for (const rule of this.rules) if (rule.matches(input)) return rule.response;
    return this.queue.shift();
  }

  nextDefault(): Case<Output> | undefined {
    if (!this.defaults.length) return undefined;
    return this.defaults[Math.min(this.defaultCount++, this.defaults.length - 1)];
  }
}

/** Per-Flow script whose longer matching prefixes take precedence. */
export class FlowScript<Input, Output> {
  readonly #owner: ScriptedOperation<Input, Output>;
  readonly #script: Script<Input, Output>;

  /** Capture one Flow prefix and its owning operation. */
  constructor(owner: ScriptedOperation<Input, Output>, script: Script<Input, Output>) { this.#owner = owner; this.#script = script; }

  /** Queue one case per matching call. */
  respond(...cases: Case<Output>[]): this { this.#owner.addResponses(this.#script, cases); return this; }
  /** Use a persistent input predicate before queued cases. */
  when(matches: (input: Input) => boolean, response: Case<Output>): this { this.#owner.addRule(this.#script, matches, response); return this; }
  /** Replace fallback cases; the last case repeats. */
  default(...cases: Case<Output>[]): this { this.#owner.setDefaults(this.#script, cases); return this; }
}

interface ResolvedCase<Output> { response: Case<Output>; branch?: BranchID; isReplay?: boolean; isUnscripted?: boolean }

/** Shared script, call log, and idempotency memory for one operation mock. */
export class ScriptedOperation<Input, Output> {
  readonly #definition: QueryDefinition;
  readonly #isMutation: boolean;
  readonly #options: MockOptions<Input, Output>;
  readonly #unscoped = new Script<Input, Output>();
  readonly #flowScripts = new Map<string, { script: Script<Input, Output>; publicScript: FlowScript<Input, Output> }>();
  readonly #terminalByKey = new Map<string, ResolvedCase<Output>>();
  readonly #calls: MockCall<Input>[] = [];
  readonly #failures: string[] = [];
  #isStrict = false;

  /** Validate the real operation definition and capture test checks. */
  constructor(definition: QueryDefinition, isMutation: boolean, options: MockOptions<Input, Output> = {}) {
    if (isMutation) validateMutationDefinition(definition); else validateQueryDefinition(definition);
    if (options.answerCheck && !definition.branches.some(branch => branch.id === options.answerCheck?.branch)) throw new TypeError('mock answer check branch is not declared');
    this.#definition = definition;
    this.#isMutation = isMutation;
    this.#options = options;
  }

  /** Queue cases on the unscoped script. */
  respond(...cases: Case<Output>[]): this { this.addResponses(this.#unscoped, cases); return this; }
  /** Add a persistent input predicate on the unscoped script. */
  when(matches: (input: Input) => boolean, response: Case<Output>): this { this.addRule(this.#unscoped, matches, response); return this; }
  /** Replace unscoped fallback cases; the last repeats. */
  default(...cases: Case<Output>[]): this { this.setDefaults(this.#unscoped, cases); return this; }
  /** Require a Respond or When case for every call, ignoring all defaults. */
  strict(): this { this.#isStrict = true; return this; }
  /** Select a script by exact Flow ID or prefix; longest prefixes win. */
  forFlow(flowIdOrPrefix: string): FlowScript<Input, Output> {
    if (!flowIdOrPrefix.trim()) throw new TypeError('mock ForFlow requires a Flow ID or prefix');
    const existing = this.#flowScripts.get(flowIdOrPrefix);
    if (existing) return existing.publicScript;
    const script = new Script<Input, Output>();
    const publicScript = new FlowScript(this, script);
    this.#flowScripts.set(flowIdOrPrefix, { script, publicScript });
    return publicScript;
  }
  /** Copy answered calls in answer order, including retries and replays. */
  calls(): MockCall<Input>[] { return [...this.#calls]; }
  /** Copy scripting errors; callers can assert after a real Flow completes. */
  failures(): string[] { return [...this.#failures]; }
  /** Throw after integration verification if a script was invalid or unscripted. */
  assertNoFailures(): void { if (this.#failures.length) throw new Error(this.#failures.join('\n')); }

  /** Add validated ordered responses to a script. */
  addResponses(script: Script<Input, Output>, cases: readonly Case<Output>[]): void { for (const response of cases) this.#reportCaseProblem(response); script.queue.push(...cases); }
  /** Add a validated persistent predicate to a script. */
  addRule(script: Script<Input, Output>, matches: (input: Input) => boolean, response: Case<Output>): void {
    if (!matches) throw new TypeError('mock When requires an input predicate');
    this.#reportCaseProblem(response);
    script.rules.push({ matches, response });
  }
  /** Replace a script's fallback cases. */
  setDefaults(script: Script<Input, Output>, cases: readonly Case<Output>[]): void {
    for (const response of cases) this.#reportCaseProblem(response);
    script.defaults = [...cases];
    script.defaultCount = 0;
  }

  /** Answer one real SDK invocation and record its safe outcome. */
  answer(call: Call, input: Input, idempotencyKey: string): ResolvedCase<Output> {
    const flowId = call.context.flowId;
    let resolved: ResolvedCase<Output>;
    const requestFailure = this.#options.requestCheck?.(input);
    if (requestFailure) {
      resolved = { response: { kind: 'branch', branch: DefectBranchID, value: undefined as Output, failure: this.#completeFailure(requestFailure) }, branch: DefectBranchID };
    } else {
      const remembered = idempotencyKey ? this.#terminalByKey.get(idempotencyKey) : undefined;
      if (remembered) resolved = { ...remembered, isReplay: true };
      else {
        resolved = this.#selectCase(flowId, input);
        const answerCheck = this.#options.answerCheck;
        if (answerCheck && resolved.response.kind === 'branch' && resolved.branch === answerCheck.branch) {
          try { answerCheck.check(input, resolved.response.value); }
          catch { this.#reportFailure('connector mock answer does not match request'); resolved = this.#defectCase('connector mock answer does not match request'); }
        }
        if (idempotencyKey && resolved.response.kind !== 'retry') this.#terminalByKey.set(idempotencyKey, resolved);
      }
    }
    const response = resolved.response;
    const record: MockCall<Input> = {
      connectorId: this.#definition.operation.connectorId, operationId: this.#definition.operation.operationId,
      connectionName: call.connection.name, flowId, callId: call.id, input,
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(resolved.branch ? { branch: resolved.branch } : {}),
      ...(response.kind === 'retry' ? { retry: true } : {}),
      ...(resolved.isReplay ? { replay: true } : {}),
      ...(resolved.isUnscripted ? { unscripted: true } : {}),
    };
    this.#calls.push(record);
    return resolved;
  }

  #selectCase(flowId: string, input: Input): ResolvedCase<Output> {
    const scripts = [...this.#flowScripts.entries()].filter(([prefix]) => flowId.startsWith(prefix)).sort(([left], [right]) => right.length - left.length).map(([, entry]) => entry.script);
    scripts.push(this.#unscoped);
    for (const script of scripts) {
      const response = script.nextScripted(input);
      if (response) return this.#resolveCase(response);
    }
    if (!this.#isStrict) for (const script of scripts) {
      const response = script.nextDefault();
      if (response) return this.#resolveCase(response);
    }
    this.#reportFailure(`unscripted call for Flow ${flowId}`);
    return { ...this.#defectCase('unscripted connector mock call'), isUnscripted: true };
  }

  #resolveCase(response: Case<Output>): ResolvedCase<Output> {
    const problem = this.#caseProblem(response);
    if (problem) return this.#defectCase(problem);
    if (response.kind === 'retry') return { response: { ...response, failure: this.#completeFailure(response.failure) } };
    if (response.kind === 'uncertain') return { response: { ...response, failure: this.#completeFailure(response.failure) }, branch: UncertainBranchID };
    return { response: response.failure ? { ...response, failure: this.#completeFailure(response.failure) } : response, branch: response.branch };
  }

  #defectCase(message: string): ResolvedCase<Output> {
    return { response: { kind: 'branch', branch: DefectBranchID, value: undefined as Output, failure: { kind: 'LOCAL_DEFECT', provider: this.#definition.operation.connectorId, operation: this.#definition.operation.operationId, message } }, branch: DefectBranchID };
  }

  #reportCaseProblem(response: Case<Output>): void { const problem = this.#caseProblem(response); if (problem) this.#reportFailure(problem); }

  #caseProblem(response: Case<Output>): string | undefined {
    if (response.kind === 'branch' && !this.#definition.branches.some(branch => branch.id === response.branch)) return `branch ${response.branch} is not declared by the operation`;
    if (response.kind === 'uncertain' && (!this.#isMutation || !this.#definition.branches.some(branch => branch.id === UncertainBranchID))) return 'an uncertain case requires a mutation uncertain branch';
    if (response.kind === 'retry' && (!Number.isSafeInteger(response.retryAfterMs) || response.retryAfterMs < 0)) return 'the retry delay is invalid';
    if ('failure' in response && response.failure && !FailureSchema.safeParse(this.#completeFailure(response.failure)).success) return 'the case failure is invalid';
    return undefined;
  }

  #completeFailure(failure: Failure): Failure { return { ...failure, provider: failure.provider || this.#definition.operation.connectorId, operation: failure.operation || this.#definition.operation.operationId }; }
  #reportFailure(message: string): void { const safeMessage = `connector mock ${this.#definition.operation.connectorId}.${this.#definition.operation.operationId}: ${message}`; this.#failures.push(safeMessage); this.#options.onFailure?.(safeMessage); }
}

/** Scripted query that implements the same invoke contract as a provider query. */
export class MockQuery<Input, Output> extends ScriptedOperation<Input, Output> implements QueryOperation<Input, Output> {
  readonly #definition: QueryDefinition;
  /** Construct from the real query definition. */
  constructor(definition: QueryDefinition, options?: MockOptions<Input, Output>) { super(definition, false, options); this.#definition = definition; }
  /** Return the real query definition. */ definition(): QueryDefinition { return this.#definition; }
  /** Answer one query call from the script. */
  invoke(call: Call, input: Input): QueryAttempt<Output> {
    const response = this.answer(call, input, '').response;
    if (response.kind === 'retry') return newQueryRetry(response.failure, response.retryAfterMs);
    if (response.kind === 'uncertain') return newQueryBranch(DefectBranchID, undefined as Output, zeroReceipt(), { kind: 'LOCAL_DEFECT', provider: call.operation.connectorId, operation: call.operation.operationId, message: 'query mock cannot return uncertainty' });
    return newQueryBranch(response.branch, response.value, zeroReceipt(), response.failure);
  }
}

/** Scripted mutation with provider-like terminal idempotency replay. */
export class MockMutation<Input, Output> extends ScriptedOperation<Input, Output> implements MutationOperation<Input, Output> {
  readonly #definition: MutationDefinition;
  /** Construct from the real mutation definition. */
  constructor(definition: MutationDefinition, options?: MockOptions<Input, Output>) { super(definition, true, options); this.#definition = definition; }
  /** Return the real mutation definition. */ definition(): MutationDefinition { return this.#definition; }
  /** Use the stable SDK call ID as the provider deduplication key. */ idempotencyKey(callId: string): string { return callId; }
  /** Answer one mutation call and remember terminal outcomes by key. */
  invoke(call: Call, input: Input): MutationAttempt<Output> {
    const response = this.answer(call, input, call.idempotencyKey ?? '').response;
    if (response.kind === 'retry') return newMutationRetry(response.failure, response.retryAfterMs);
    if (response.kind === 'uncertain') return newMutationUncertain(response.value, response.failure, zeroReceipt());
    return newMutationBranch(response.branch, response.value, zeroReceipt(), response.failure);
  }
}

/** Construct a scripted query from a real operation definition. */
export function newQuery<Input, Output>(definition: QueryDefinition, options?: MockOptions<Input, Output>): MockQuery<Input, Output> { return new MockQuery(definition, options); }
/** Construct a scripted mutation from a real operation definition. */
export function newMutation<Input, Output>(definition: MutationDefinition, options?: MockOptions<Input, Output>): MockMutation<Input, Output> { return new MockMutation(definition, options); }
