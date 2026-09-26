# Dex Connectors Library

## Overview

[![Dex Connectors directory](https://superdurable.github.io/dex-connectors-library/card.png)](https://superdurable.github.io/dex-connectors-library/)

Dex Connectors Library is the open-source boundary between Dex Flows and
external providers. Credentials stay outside durable Flow state. Provider call
identity comes from Dex Step execution identity, and every provider outcome is
explicit.

The directory lists each published connector, its company, reusable UI units,
triggers, and operations. Search matches all of those names and descriptions.

## Use a connector

Connector operations are typed Dex Steps. Map the current Flow value to the
provider input, then connect the successful provider branch to the next Step:

```go
dex.DefineStep(slack.NewPostThreadReplyStep(slack.PostThreadReplyStepConfig[ThreadState]{
	StepType:       postCompletionStepType,
	ConnectionName: ConnectionName,
	ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
		ID: "completionText", UnitID: slack.UIUnitTextInput,
		Label: "Completion reply", Required: true,
		Bindings: []sdkgo.ConnectorUIBinding{{
			Port: slack.UITextInputPortText, JSONPointer: "/text",
		}},
	}}},
	Connection:     flow.connection,
	MapToOperationInput: func(state ThreadState) slack.PostThreadReplyInput {
		return slack.PostThreadReplyInput{
			ChannelID:       state.Input.ChannelID,
			ThreadTimestamp: state.Input.ThreadTimestamp,
			Text:            flow.postCompletionConfiguration.Value.Text,
		}
	},
	Sent: sdkgo.GoTo(completionPosted{}),
}))
```

Follow the [Slack thread approval walkthrough](connectors/slack/examples/thread-approval/README.md)
to configure Slack and Dex Web, run the example Worker, and verify the complete
path: a top-level Slack message starts a Flow, the Flow reads the thread, an
allowed reply invokes its typed RPC, and the Connector posts the completion
reply. The [complete Flow source](connectors/slack/examples/thread-approval/flow/workflow.go)
shows how the Steps, triggers, and RPC are wired together.

In Dex Web, open **Connections**, select `slack / slack-workspace`, and choose
**Authorize**. Then configure each Flow use in the left panel: set the
`postThreadReply` **Completion reply**, configure both trigger bindings, and
choose **Save** in each unit. A green check marks saved setup. In the screenshot,
authorization and both triggers are ready; `postThreadReply` is the remaining
item to configure before starting the example Worker.

![Dex Web Slack connection setup with authorization, operation, and trigger status](docs/assets/slack-connection-setup.png)

## Organization

The first folder under `connectors/` is the company name, matches
`metadata.company`, and contains `logo.svg`. Connectors for the same company
are grouped below that folder, such as `connectors/google/gmail` and
`connectors/google/spreadsheet`.

Each connector is its own Go module. Release tags use the module directory,
such as `connectors/github/vX.Y.Z` and `connectors/google/gmail/vX.Y.Z`.
The manifest `metadata.version` is the release version. The root
`connectors.yaml` registry is the source for the public catalog.

- [Connector contract](docs/connector-contract.md)
- [Architecture](docs/architecture.md)
- [Manifest authoring](docs/manifest-authoring.md)
- [Versioning and releases](docs/versioning-and-releases.md)
- [Acceptance](docs/acceptance.md)
