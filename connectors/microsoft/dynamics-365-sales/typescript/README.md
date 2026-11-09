# Dynamics 365 Sales TypeScript connector

`MicrosoftDynamics365SalesClient.findContact()` binds the `findContact` Query. `FindContactInput.emailAddress` selects an exact primary email; `Contact` returns `contactId`, `fullName`, and `emailAddress`. The client obtains a fresh Entra client-credentials token for each attempt. Follow the [connection guide](../README.md) and run the [example Flow](examples/contact-lookup/README.md). The package replays the Go-recorded provider scenarios. Live Microsoft authorization has not been verified.
