# Text message delivery

Configure the static `twilio-messaging` connection in Dex Web as described in
[the connector README](../../README.md). From the TypeScript package directory,
run `npm ci`, start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC
address, and run `npm run example:run`. Start `TwilioTextMessageDelivery` with
`{"to":"+14155550123","body":"Ready for pickup."}`. The Flow sends one
message and reads its current status. Delivery may still be queued.

For a credential-free real Dex check, run `npm run example:integration` against
the same server. It uses the generated mock with a read retry and checks that
Dex completes the Flow with one send. No provider request is made.

Generate a strict Flow Definition Graph 2.0 from this directory with
`dexcli visualize examples/text-message-delivery/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/twilio-messaging-typescript-fdg`.
The generated Step factories carry the provider identities; `flow.ts` gives
each Step a group and explanation.
The local Dex CLI currently reports `dynamic_step_target` for the generated
Step instances and `dynamic_class` for the factory target supplied to the
completion Step. Real Worker and Client execution passes; strict FDG output
remains unverified until Dex CLI can analyze this factory pattern.
