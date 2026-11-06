# DocuSign envelope status Flow

This Flow reads one envelope's status through the static `docusign-envelope-reader` connection. Configure that connection in Dex Web using either the production or developer OAuth method described in the [DocuSign connector README](../../../README.md). The connected user must belong to the account holding the envelope.

From `connectors/docusign/typescript`, run `npm ci` and `npm run example:envelope` to start the Worker. In another terminal, run `npm run example:envelope:start -- examples/envelope-status/input.example.json` to submit the Flow. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault address. Change `envelopeId` in the JSON file to a GUID for an envelope visible to the connected user.

`npm run test:integration` exercises the same Flow through a real Dex Worker and Client against a local stand-in. The test uses an OS-assigned provider port and requires a running `dexcli dev` server named by `DEX_FLOW_SERVICE_ADDRESS`.

To validate the Flow Definition Graph 2.0 annotations, run `dexcli visualize examples/envelope-status/flow/workflow.ts --schema-version 2.0 --lint app --json --out -` from this package after installing its dependencies and building the package.
