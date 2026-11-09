# Dynamics 365 Sales Python connector

`Client.find_contact()` binds the `findContact` Query. `FindContactInput.email_address` selects an exact primary email; `Contact` returns `contact_id`, `full_name`, and `email_address`. The client obtains a fresh Entra client-credentials token for each attempt. Follow the [connection guide](../README.md) and run the [example Flow](examples/contact_lookup/README.md). The package replays the Go-recorded provider scenarios. Live Microsoft authorization has not been verified.
