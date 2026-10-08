# `@superdurable/dex-connectors-react`

This package holds the shared pieces of Connector Studio bundles: connection
state, the Studio Host API client, Dex Web styling, a live model picker, and
the OpenAI, Claude, and Gemini model lists that picker bundles share.
The browser receives a logical provider name, a normalized status, and
provider responses the host has already checked for credential material;
secret values and OAuth tokens remain on the server.

```tsx
<ConnectionStatus
  provider="Google Sheets"
  state="expired"
  onReconnect={() => beginOAuth()}
/>
```

The package also defines Connector Studio Host API 0.2 for sandboxed bundles.
The host selects a `connection` surface or one `configurationUnit` surface.
Unit targets contain the Flow-owned instance identity, operation or Trigger
scope, port bindings, and only that unit's current values. A bundle saves port
values with `use.configuration.save`; Dex Web applies the declared bindings to
the isolated use configuration. A `connection` target with a `unitId` renders
that unit inline in the connection form for one connection configuration
field. Its `bindings` bind the unit's output port to the field, its `value`
holds the field's stored value, and `use.configuration.save` updates that
field. Bundles call
`observeConnectorStudioFrameAutoHeight(hostReady)` from their UI lifecycle so a
`ResizeObserver` sends bounded `connector.frame.resize` messages whenever the
selected surface changes height. The host can then fit the sandbox iframe
without an inner scrollbar.

Every message carries the connector ID, protocol version, and session nonce.
Command messages additionally carry request identity. Hosts also validate the
iframe `Window` source and declared backend capability before executing a
command. Messages never carry stored provider credentials.

### Connection setup surface

A release whose `studio.setup.backendCapabilities` declares
`connection.write` (exported as `connectorConnectionWriteCapability`) owns the
whole connection form. A host that supports it renders the `connection`
target without a `unitId` in place of its own form, and grants
`connection.write` in the ready message. The bundle then collects the
configuration and any credential the user types, and saves both with one
`connection.save` command through `saveConnectorConnection(client, save)`.
`save.configuration` is the complete non-secret configuration; a field it
omits is cleared. `save.credentials` holds newly typed values, and
`save.keepCredentialFields` names stored fields to keep. The ready message's
`connection.storedCredentialFields` lists which fields hold a saved value,
never the value itself, so the bundle can offer "leave blank to keep".

Before saving, the bundle can run a declared provider command with the typed
value: `withDraftCredentials(client, {api_key: typed})` returns a client whose
`executeProviderCommand` sends the value as `credentials`. The host forwards it
to its broker, which uses it in place of the stored value for that one call
and stores nothing. Existing model-list loaders therefore list models before
the first save. Hosts refuse `credentials` from a bundle without
`connection.write`.

A setup bundle receives typed credentials, so it must send them only in these
two commands, keep them out of logs, markup, and storage, and clear a typed
value when the user switches to another provider. Dex Web serves a
`connection.write` bundle without remote images. `ModelPickerStudioApp` takes
the surface through `renderConnectionSetup`; on a host that does not grant
`connection.write` it keeps the status card and the host form.

## Studio client and styling

`useConnectorStudioClient(connectorId)` waits for the host's ready message and
returns `send` and `executeProviderCommand`. Results are matched by request ID
and session nonce, and a rejected command raises
`ConnectorStudioCommandError` with the host's error code.
`client.ready.connection` is a `ConnectorStudioConnection`. Beside the
connection state it carries `authMethodIds`, which holds the auth method the
connection selected, and `configuration`, the connection's stored non-secret
configuration. Hosts that predate them omit both. The client then reports `[]`
and `{}`, and `isConfigurationReported` is `false`. `storedCredentialFields`
names the credential fields with a saved value, and is `[]` from hosts that
predate it.
`collectProviderPages` follows provider cursors with a page cap, stops on a
repeated cursor, and reports `isTruncated`.

`applyConnectorStudioTheme(hostReady)` installs the Studio stylesheet and
selects the theme. It uses Dex Web's default light theme unless the host
reports `theme: "dark"`. `StudioSurface`, `StudioHeader`, `StudioField`,
`StudioButton`, and `StudioNotice` render the shared card, heading, labelled
control, CTA-green button, and inline status. `StudioField` renders its hint
outside the `<label>` and adds the hint to a single child control's
`aria-describedby`, so the hint describes the control without becoming part of
its accessible name.

### Host stylesheet

A bundle inlines this package when it is built, so the compiled
`connectorStudioStyles` change only when a connector is released. Dex Web owns
the canonical copy of the same rules
(`web/app/v2/connections/connectorStudio.css` in the Dex repository) and sends
it in every ready message, with its theme and token values. The message is
shown here without its other fields, and with a shortened stylesheet:

```json
{
  "type": "connector.host.ready",
  "theme": "dark",
  "stylesheet": ":root { --studio-cta: #008650; } .studio-surface { display: grid; } ...",
  "themeTokens": {
    "--studio-surface-card": "#1a1c1e",
    "--studio-cta": "#70eea9",
    "--studio-focus-ring": "0 0 0 3px rgba(137, 190, 255, 0.3)"
  }
}
```

`applyConnectorStudioTheme` writes `stylesheet` into its one managed `<style>`
element when `isConnectorStudioStylesheet` accepts it: a string of at most
`connectorStudioStylesheetMaxLength` (65,536) UTF-16 code units that contains
no `</` and has a `.studio-*` selector for every class in the bundle's
`connectorStudioClassNames`. Otherwise it writes `connectorStudioStyles`. That
covers hosts that predate the field, and it keeps a bundle built with a class
that an older Dex Web does not know on the stylesheet it was built with. So a
rule-level Dex Web restyle reaches every released bundle without a connector
release. Only new markup, such as a new component or class, needs one.

The bundle checks nothing else, because its checks are frozen into every
release. The host stylesheet and `connectorStudioStyles` follow these
authoring rules, which Dex Web tests for its copy and this package tests for
the compiled one:

- Each selector is `:root`, `:root[data-theme='dark']`, `html`, or `body`, or
  starts with a class from `connectorStudioClassNames`. Any other class it
  names is also a contract class. Element, attribute, and pseudo-class parts
  may follow, such as `.studio-surface input[type='search']` or
  `.studio-button:hover:not(:disabled)`.
- Custom properties are declared only in `:root` rules and only for names in
  `connectorStudioThemeTokenNames`. `var()` refers only to those names.
- The stylesheet has no at-rules, `url()`, `image-set()`, `expression()`,
  backslash escapes, `!important`, or `</`. `!important` would override the
  host's tokens, and an escape could spell a forbidden function.
- It styles every class in `connectorStudioClassNames`.

### Theme tokens

`applyConnectorStudioTheme` applies the tokens after the stylesheet, as inline
custom properties on `document.documentElement`, so they override the token
defaults of either stylesheet. A token is accepted only when its name is in
`connectorStudioThemeTokenNames`, the `--studio-*` tokens that
`connectorStudioStyles` declares, and `isConnectorStudioThemeTokenValue`
accepts its value. Dex Web validates the tokens it sends with the same grammar
(`web/app/v2/connections/connectorStudioTheme.ts` in the Dex repository):

- Colours are `#` with 3, 4, 6, or 8 hex digits, `rgb(R, G, B)`, or
  `rgba(R, G, B, A)`, in comma syntax. Channels are integers from 0 to 255
  without leading zeros. Alpha is 0 to 1 with at most four decimals.
- Radii are `0` or a non-negative `px` length with at most three digits on
  each side of the decimal point.
- `--studio-focus-ring` is one `box-shadow`: an optional `inset`, two to four
  `px` lengths that may be negative, and one colour.
- `--studio-duration` is up to four digits of `ms`, or seconds with at most two
  integer digits and three decimals, such as `160ms` or `.16s`.
- A font stack has 1 to 16 comma-separated families. A family is a quoted name
  of letters, digits, spaces, and hyphens, or unquoted words that each start
  with a letter, optionally after one hyphen. The CSS-wide keywords, `default`,
  and `none` are accepted only when quoted.
- A value is at most 256 characters.

Unknown names and any other value, including `transparent`, `none`, `url()`,
`var()`, `calc()`, semicolons, braces, and `!important`, are ignored one token
at a time. Every token the host does not override keeps the stylesheet's
default, so bundles still look right under hosts that send no tokens. Call
`applyConnectorStudioTheme` for every ready message, as the shared bundles do
from an effect on `client.ready`: each call replaces the previous host
stylesheet and tokens, and a ready message without them restores
`connectorStudioStyles` and the defaults.

### Studio class contract

`connectorStudioClassNames` lists every class a bundle may render. Bundles
bring no styles of their own; they render these classes, and the host's
stylesheet decides how they look. Rules also reach some elements inside a
class, so bundles keep this markup:

| Class | Markup |
| --- | --- |
| `studio-surface` | The card around one surface, from `StudioSurface`. Text, search, and untyped `input` elements and `select` elements inside it get the field style. |
| `studio-header` | A surface heading, from `StudioHeader`: an optional `img` icon, then a `div` with an `h1` or `h2` title and an optional `p` description. |
| `studio-muted` | Secondary text. |
| `studio-field` | A labelled control, from `StudioField`: a `label` holding the name and the control, then an optional `small` hint. |
| `studio-actions` | A row of buttons and short status text. |
| `studio-button`, `studio-button-primary` | A button, from `StudioButton`. The primary class adds the CTA green. |
| `studio-notice`, `studio-notice-info`, `studio-notice-success`, `studio-notice-error`, `studio-notice-attention` | An inline message with one tone class, from `StudioNotice`. |
| `studio-options` | A scrolling list of choices, usually a `fieldset`. |
| `studio-option` | One choice: a `label` whose first child is a radio or checkbox `input`. |
| `studio-option-label` | The choice's name, with an optional leading `img`. |
| `studio-option-id` | The choice's provider ID, in the monospace font. |
| `studio-option-detail` | One line of detail about the choice. |
| `studio-badges`, `studio-badge` | A row of short capability tags, and one tag. |
| `studio-checkbox` | A `label` holding a checkbox `input` and its text. |

Adding a class is additive: add it to `connectorStudioClassNames`,
`connectorStudioStyles`, and Dex Web's stylesheet. Until the host styles it,
bundles built with it keep their compiled stylesheet. For the same reason, a
host stylesheet that drops or renames a class sends every released bundle
built with that class back to its compiled stylesheet, so Dex Web keeps
styling a class while released bundles know it. The tests check that the
shared components render exactly these classes and that
`connectorStudioStyles` styles each of them and no other class.

Bundles must not inject their own styles. A bundle that links this
package with a `file:` dependency must set
`resolve.dedupe: ["react", "react-dom"]` in its Vite config, because this
package has its own React installed and the client hooks need the bundle's
single copy. CI runs
`script/studio_bundle_theme_check.py`, which fails when a connector UI injects
or links a stylesheet, sets inline styles, or writes raw HTML; when it renders
a class outside `connectorStudioClassNames` through a JSX `className` attribute
or a `className` object property, such as `createElement` props or a JSX
spread; when it computes a `className` or sets classes through the DOM; when
it calls `applyConnectorStudioTheme` anywhere but a React effect that passes
the ready message and depends on it; when it calls neither that function nor
`mountModelPickerBundle`; or when it does not dedupe React.

## Live model picker

`ModelPicker` is the configuration unit that LLM connectors share. The
connector declares a read-only `listModels` provider command and passes a
`loadModels` function that runs it through the host broker and projects the
provider's JSON into `ModelOption` values. Because the list is live, a model
the provider adds appears without a connector release. Models the connector's
filter judges unsuitable, such as embedding models, stay behind "Show all
models". The user can also keep the connection's default model, saved as an
empty `model`, or type any model ID when listing fails. When the list fails or
lacks the saved model, the entry shows the saved model unless the user already
chose or typed one while the list loaded or before Retry, so Save sends the
model the picker shows.

```tsx
<ModelPicker
  target={hostReady.target}
  providerName="Gemini"
  loadModels={loadGeminiModels}
  onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
/>
```

A loader may return `notices` with its `ModelListing`, such as one
`attention` notice for each source of a combined list that could not be
listed. `ModelPicker` shows them in order with `StudioNotice` once the list
loads.

`validateManualModel` checks a typed model ID, with surrounding whitespace
trimmed, before it can be saved. It returns a message for an invalid ID, which
the picker shows below the entry while Save is disabled, or `undefined` to
accept it. It never checks a model chosen from the list, the connection
default, or a blank entry, and it runs on every render, so it must be cheap and
must not throw.
`manualModelPlaceholder` replaces the entry's `model-id` placeholder.
`mountModelPickerBundle` passes both options to the picker. Without notices
and these options, the picker renders exactly as before.

`validateModelIDForRule(rule, value)` applies the Go SDK's
`textgen.ModelIDRule.ValidateModelID` in the browser, so a picker rejects
exactly the model IDs a connector's `generateText` would reject as `defect`.
`rule` is `"body"` or `"pathSegment"`, as `textgen.ModelIDRule`'s `String`
method spells it. The result is `{isValid: true, modelId}`, where `modelId` is the canonical
ID the provider receives, or `{isValid: false, message}`, whose message never
repeats the value. Both rules first trim the whitespace Go's
`strings.TrimSpace` trims, which differs from `String.prototype.trim` for
U+0085 and U+FEFF. `"body"` then requires 1 to 256 printable ASCII characters
without spaces; `"pathSegment"` removes one leading `models/` and requires
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. The package's tests run every case in
[`sdkgo/textgen/textgentest/testdata/model_id_cases.json`](../../sdkgo/textgen/textgentest/testdata/model_id_cases.json).

```tsx
mountModelPickerBundle({
  connectorId: "gemini", providerName: "Gemini", iconUrl: "./icon.svg", loadModels: loadGeminiModels,
  validateManualModel: (model) => {
    const validation = validateModelIDForRule("pathSegment", model);
    return validation.isValid ? undefined : validation.message;
  },
});
```

### Connection context

`mountModelPickerBundle` calls `loadModels(client, connection)` with the ready
message's `ConnectorStudioConnection`, whose `configuration` holds the
connection's saved non-secret settings. A connector that serves several
providers, such as `llm`, keeps one provider per connection, so its loader
reads the saved `provider` and runs only that provider's list commands, and
the API key reaches only that provider's hosts. Before the provider is saved,
and on hosts that report no configuration, the loader runs no command and
returns a notice; the picker still offers the default option and model ID
entry. Existing one-argument loaders work unchanged. The picker loads again on
Retry and when the session, `authMethodIds`, or the connection's saved
configuration change, so a new provider lists its own models.

On a Step or Trigger unit, the first option saves an empty `model`, which
inherits the connection's model. The bundle labels it
`Connection default (<model>)` with the connection's `configuration.model`,
such as `Connection default (claude-sonnet-5)`. When the host reports a
configuration without a model, the label names `defaultModelDescription`
instead, such as `Connection default (the provider's default model)`. On hosts
that report no configuration, and when neither names the default, the option
keeps its label "Use the connection's default model".

For a `connection` target whose `unitId` is `modelPicker`, the bundle renders
`ModelPicker` for the connection's model field. It reads the saved model at the
binding of the unit's `model` port and saves the pick as
`use.configuration.save` with `{value: {model}}`. There the empty option means
the connector's own default, labelled `Connector default (<description>)` with
`defaultModelDescription`, or "Use the connector's default model" without it.
A `connection` target without a `unitId` still renders the connection status
card. A bundle that renders `ModelPicker` itself passes the same labels with
`defaultModelOptionLabel`.

This example is tested in `test/model-picker-connection-context.test.tsx`:

```ts
import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadClaudeModelListing, loadOpenAIModelListing } from "@superdurable/dex-connectors-react/provider-model-lists";

mountModelPickerBundle({
  connectorId: "llm", providerName: "LLM", iconUrl: "./icon.svg",
  defaultModelDescription: "the provider's default model",
  loadModels: async (client, connection) => {
    switch (connection.configuration.provider) {
      case "openai":
        return loadOpenAIModelListing(client, {capability: "llm.models-list", commandId: "listOpenAIModels"});
      case "anthropic":
        return loadClaudeModelListing(client, {capability: "llm.models-list", commandId: "listAnthropicModels"});
      default: {
        const message = connection.isConfigurationReported
          ? "Save the connection's provider to list its models, or enter a model ID."
          : "This Dex Web release does not report the connection's provider. Enter a model ID.";
        return {models: [], notices: [{tone: "attention", message}]};
      }
    }
  },
});
```

## Provider model lists

The `@superdurable/dex-connectors-react/provider-model-lists` subpath exports
the OpenAI, Claude, and Gemini model list loaders and their pure projections.
Each loader takes the command IDs and capability from its caller, so every
connector that declares the same provider list request shares one projection
instead of copying provider semantics, whatever it names its commands.

| Loader | Manifest command it runs | Projection |
| --- | --- | --- |
| `loadOpenAIModelListing(client, {capability, commandId}, now?)` | `GET https://api.openai.com/v1/models` with a bearer key. | `projectOpenAIModelList(value, now)` lists newest first by `created`. It hides non-text families, judged by the base model of an `ft:` ID, and models whose `shutdown_date` is on or before `now`, which defaults to the time the list returns. It notes every announced shutdown date. |
| `loadClaudeModelListing(client, {capability, commandId})` | `GET https://api.anthropic.com/v1/models` with `fixedHeaders: {anthropic-version: "2023-06-01"}` and an `afterId` query parameter that targets `after_id`. | `projectClaudeModelPage(page)` keeps Claude's order, hides nothing, labels by `display_name`, details the context window and output limit, and badges `capabilities`. `readNextClaudeModelCursor(page)` returns `last_id` while `has_more` is true. The loader drops repeated IDs across pages. |
| `loadGeminiModelListing(client, {capability, nativeCommandId, openAICompatibleCommandId})` | The native `GET https://generativelanguage.googleapis.com/v1beta/models` with the key in `x-goog-api-key` and a `pageToken` query parameter, then, when the host rejects it, the bearer `GET .../v1beta/openai/models`. | `projectNativeGeminiModels(items)` strips `models/` and hides models without `generateContent` or with a non-text family ID. `projectOpenAICompatibleGeminiModelList(value)` strips `models/` and hides non-text family IDs. |

`ProviderModelListCommand` and `GeminiModelListCommands` type the command
arguments. The Claude and Gemini loaders follow cursors with
`collectProviderPages`. A loader rejects with the host's error when a page it
needs fails, after Gemini's fallback to its OpenAI-compatible command, so
`ModelPicker` offers manual model entry. A bundle inlines this package when it
is built, so a released bundle keeps the projection it was built with, and a
projection change reaches a connector at its next release.

```ts
import { mountModelPickerBundle } from "@superdurable/dex-connectors-react";
import { loadClaudeModelListing } from "@superdurable/dex-connectors-react/provider-model-lists";

mountModelPickerBundle({
  connectorId: "claude", providerName: "Claude", iconUrl: "./icon.svg",
  loadModels: (client) => loadClaudeModelListing(client, {capability: "claude.models-list", commandId: "listModels"}),
});
```
