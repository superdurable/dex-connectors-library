# `@superdurable/dex-connectors-react`

This package holds the shared pieces of Connector Studio bundles: connection
state, the Studio Host API client, Dex Web styling, and a live model picker.
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
the isolated use configuration. Bundles call
`observeConnectorStudioFrameAutoHeight(hostReady)` from their UI lifecycle so a
`ResizeObserver` sends bounded `connector.frame.resize` messages whenever the
selected surface changes height. The host can then fit the sandbox iframe
without an inner scrollbar.

Every message carries the connector ID, protocol version, and session nonce.
Command messages additionally carry request identity. Hosts also validate the
iframe `Window` source and declared backend capability before executing a
command. Messages never carry provider credentials.

## Studio client and styling

`useConnectorStudioClient(connectorId)` waits for the host's ready message and
returns `send` and `executeProviderCommand`. Results are matched by request ID
and session nonce, and a rejected command raises
`ConnectorStudioCommandError` with the host's error code.
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
empty `model`, or type any model ID when listing fails.

```tsx
<ModelPicker
  target={hostReady.target}
  providerName="Gemini"
  loadModels={loadGeminiModels}
  onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
/>
```
