# Ashby candidate inspection Flow

This runnable Flow calls `getCandidate` through the static `ashby` connection. It persists the selected candidate profile in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

The same Worker registers `AshbyCandidateRename`, which calls `updateCandidate` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Updated Candidate"}`. It retains the updated profile in an Attribute. This requires **Candidates write** on the API key; use a disposable candidate for a live run. Its optional branches are unwired.

The Worker also registers `AshbyCandidateTag`, which calls `addCandidateTag` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","tagId":"38430ede-5bd2-41fc-b474-87591cb98cbc"}`. Replace both sample UUIDs with an existing candidate and tag from Ashby; use a disposable candidate for a live write. The API key needs **Candidates write**. Its optional branches are unwired.

The Worker also registers `AshbyCandidateCreate`, which calls `createCandidate` with `{"name":"Example Candidate"}`. Supply a complete first and last name; blank is invalid. The API key needs **Candidates write**. An unknown provider outcome can produce a duplicate candidate within the three-send cap; an unconfirmed outcome at the cap selects the optional `uncertain` branch, which this example leaves unwired.

The Worker also registers `AshbyCandidateSearch`, which calls `searchCandidates` with `{"name":"Example Candidate","email":"example@example.com","limit":20}`. Find a candidate name or complete email address in Ashby **Candidates > Search**; at least one is required, and blank `limit` uses the provider's maximum of 100 results. The API key needs **Candidates read**. Optional branches are unwired.

The Worker also registers `AshbyCandidateProject`, which calls `addCandidateProject` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","projectId":"bcffca12-5b09-4a76-acf2-00a8e267b222"}`. Replace both UUIDs with an existing candidate and project from Ashby **Sourcing > Projects**; the key needs **Candidates write**. Optional branches are unwired.

The Worker also registers `AshbyCandidateNote`, which calls `createCandidateNote` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","note":"Example candidate note"}`. Find the candidate ID under **Candidates > Search**; supply plaintext up to 8192 bytes with no secrets. The API key needs **Candidates write**. A repeated unknown send can create duplicate notes, and the optional `uncertain` branch is unwired.

The Worker also registers `AshbyJobCreate`, which calls `createJob` with `{"title":"Software Engineer","teamId":"f9e52a51-a075-4116-a7b8-484deba69004","locationId":"3ae2b801-19f6-41ef-ad28-214bd731948f"}`. Replace both UUIDs with a team and location from Ashby job settings, and use a disposable job for a live run. The API key needs **Jobs write**. An unknown response can produce duplicate jobs within the three-send cap; the optional `uncertain` branch is unwired.

The Worker also registers `AshbyApplicationCreate`, which calls `createApplication` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","jobId":"1be42b8e-cafd-4beb-8121-f4981eb20f42"}`. Find both UUIDs under **Candidates > Search** and **Jobs > Jobs**. Use a disposable candidate and job for a live run. The API key needs **Candidates write**. An unknown response can create a duplicate application within the three-send cap; the optional `uncertain` branch is unwired.

## Strict FDG 2.0 generation

From `connectors/ashby/go`, run:

```bash
dexcli visualize ./examples/candidate-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-inspection-fdg
dexcli visualize ./examples/candidate-inspection/flow/rename_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-rename-fdg
dexcli visualize ./examples/candidate-inspection/flow/tag_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-tag-fdg
dexcli visualize ./examples/candidate-inspection/flow/create_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-create-fdg
dexcli visualize ./examples/candidate-inspection/flow/search_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-search-fdg
dexcli visualize ./examples/candidate-inspection/flow/project_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-project-fdg
dexcli visualize ./examples/candidate-inspection/flow/note_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-note-fdg
dexcli visualize ./examples/candidate-inspection/flow/job_workflow.go --schema-version 2.0 --json --out /tmp/ashby-job-create-fdg
dexcli visualize ./examples/candidate-inspection/flow/application_workflow.go --schema-version 2.0 --json --out /tmp/ashby-application-create-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override ashby=<artifact-directory>`. In Dex Web Connections select the `ashby` connection. Follow the [connection guide](../../../README.md) to create an API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/ashby/go`:

```bash
GOWORK=off go run ./examples/candidate-inspection
```

Start `AshbyCandidateInspection` with a unique Flow ID and input `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e"}`. Replace the sample with a candidate ID returned by Ashby. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration tests. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Ashby authorization, listing, retrieval, updating, and tag writes remain unverified.
