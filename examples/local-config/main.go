// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log"

	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdk/go/localconfig"
)

func main() {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	connection, err := gmail.NewLocalConnection(store, "sender")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("loaded Gmail connection sender from %s: %s\n", store.Path(), connection)
}
