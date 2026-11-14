# Kuaidi100 connector

`QueryTracking` reads the current status and latest-first tracking events for one carrier code and waybill number through Kuaidi100's [real-time query API](https://api.kuaidi100.com/document/5f0ffb5ebc8da837cbd8aefc.html). It is a billed Query. The provider [requires at least 30 minutes between queries for one parcel](https://api.kuaidi100.com/document/shishichaxunchanpinjieshao); automatic retries wait 30 minutes and stop after one retry. A 24-hour frequency lock is a definite rejection that applications can handle on the optional branch.

In Dex Web **Connections**, create a `kuaidi100` connection and enter the enterprise account's `customer` and `key` as secrets. The connection guide points to the provider console and its My Information > Enterprise Information page. The input fields are `carrierCode` (lowercase code from the [carrier table](https://api.kuaidi100.com/document/5f0ffb5ebc8da837cbd8aefc.html), nonsecret), `trackingNumber` (6–32 characters, potentially sensitive), and optional `phone` (sender or recipient phone; required by SF Express and ZTO). The phone and credential values are never returned, stored as a Flow result, or logged by this connector. Use the provider's current carrier table; some carriers require a phone even though the generic input is optional.

The runnable [parcel tracking Flow](go/examples/parcel-tracking/README.md) uses the static `kuaidi100` connection. It stores only the provider's tracking result in a Dex Attribute, and the generated mock uses synthetic parcel events. Live provider authorization and real carrier queries require a customer-controlled enterprise account.

```go
client, err := kuaidi100.New(
    kuaidi100.Config{},
    sdkgo.StaticCredentialProvider[kuaidi100.Credentials]{
        {Provider: "kuaidi100", Name: "kuaidi100"}: {
            Customer: sdkgo.NewSecretString(customer),
            Key: sdkgo.NewSecretString(key),
        },
    },
)
if err != nil { return err }
connection, err := kuaidi100.NewConnection(client, sdkgo.ConnectionRef{Provider: "kuaidi100", Name: "kuaidi100"})
if err != nil { return err }
_ = connection
```

For application code, use `NewProjectConnection` and the operation-specific `NewQueryTrackingStep` shown in the runnable Flow.
