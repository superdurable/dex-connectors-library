# Outbound lookup, TypeScript

This Flow lists one Postmark outbound page and retains its nonsecret metadata.
Its static Dex Web connection name is `postmark`; the provider token is never
part of the start input. Configure that connection as described in the
[Postmark authorization guide](../../../README.md#authorization), then run
`dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.

From `connectors/postmark/typescript`, install dependencies and run the Worker:

```sh
npm ci
npm run example:outbound
```

From a second terminal with the same Dex project configuration, start a Flow:

```sh
npm run example:outbound:start -- examples/outbound-lookup/input.example.json
```

The starter prints one page. The provider request runs in the generated
`ListPostmarkOutboundMessages` Step. Its optional branches are unwired and
fail the Flow. To verify retry and failure transitions against a real Dex
stack and a local provider stand-in, run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:26320 npm run test:integration
```

Run strict FDG 2.0 generation from this package, with Dex CLI 1.28.1 or newer:

```sh
dexcli visualize examples/outbound-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/postmark-typescript-fdg --open=false
```

Dex CLI 1.33.0 currently reports `dynamic_step_target` for the registered
connector QueryStep returned by the generated factory. The partial graph is
written, but strict lint remains blocked on that analyzer limitation. The
same Flow is verified through its real Worker and Client integration test.

The production Worker uses `loadFromEnvironment()` for its local Dex project.
This example has not been exercised against a live Postmark account.
