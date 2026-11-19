# Qingflow Python connector

`dex-connector-qingflow` reads application keys and names from the token's workspace through the `listApplications` Query. Configure the super-admin accessToken using [the connector guide](../README.md). The Query accepts an empty input and returns `applications`; other provider fields are discarded.

The optional `providerRejected`, `invalidResponse`, and `defect` branches are left unwired by the runnable [application directory Flow](examples/application_directory/). Seven Go-recorded scenarios cover success, terminal branches, rate limiting, service failure, and transport failure. The example integration test uses a real Dex Worker and Client with a local fake provider. Live Qingflow authorization remains unverified without an approved workspace.
