// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import io.superdurable.dex.Client;

/**
 * Invokes one application RPC through a typed Dex RPC stub.
 *
 * <pre>{@code
 * DexRpcInvocation<ApprovalFlow, ReplyInput> invocation =
 *         (client, flow, input) -> client.invokeRPC(flow::receiveReply, input);
 * }</pre>
 *
 * @param <F> the registered Flow class that declares the RPC
 * @param <I> the RPC input type
 */
interface DexRpcInvocation<F, I> {
    /**
     * Invokes the RPC on the stub's Flow.
     *
     * @param client the Dex Client
     * @param rpcStub the stub from {@link Client#newRpcStub(Class, String)} for the resolved Flow ID
     * @param input the mapped RPC input, or {@code null} without a mapper
     * @throws Exception when the invocation fails
     */
    void invokeRpc(Client client, F rpcStub, I input) throws Exception;
}
