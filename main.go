package main

import (
	"log"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint"
)

func main() {
	if err := lint.Validate("."); err != nil {
		log.Fatal(err)
	}
}
