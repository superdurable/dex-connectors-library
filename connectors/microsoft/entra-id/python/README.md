# Microsoft Entra ID Python connector

`dex-connector-microsoft-entra-id==0.1.0` provides eight Dex operations for Microsoft Graph: `getUser`, `listUsers`, `createUser`, `disableUser`, `enableUser`, `revokeSignInSessions`, `addUserToGroup`, and `removeUserFromGroup`. It requires `dex-connectors-sdk==0.6.0`. No Triggers are declared.

The [account lookup example](examples/user_lookup/flow.py) uses the static `entra-directory` connection name and the generated `new_get_user_step` factory. Start its Worker with `uv run python -m examples.user_lookup.start` after configuring that connection in Dex Web and setting `DEX_FLOW_SERVICE_ADDRESS` for your Dex server. The [integration test](examples/user_lookup/integration_test.py) runs the same Flow through a real Dex Worker and Client, with a local Microsoft Graph stand-in.

Both app-only client credentials and delegated Microsoft OAuth use the manifest-generated refresh driver and project credential source. Follow the [connector authorization guide](../README.md#authorization) to register the Microsoft application, grant the exact Graph permissions and administrator consent, and connect through Dex Web. The client never accepts or returns an initial password; `createUser` generates one for the Graph request and requires a password change on first sign-in.

Run the package and Go-recorded provider replay tests with `uv run pytest -q`. The fixture at `conformance/connectors/microsoft/entra-id/scenarios.json` covers every operation's request and result, including Retry, transport failure, and read-back behavior. Microsoft tenant authorization and provider behavior have not been verified live.
