package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint"
)

const help = "Simple lint checking that all resource attribute setters have " +
	"corresponding attributes in the resource schema.\n\n" +
	"\u001B[1mUsage:\u001B[0m\n  terraform-setter-lint \u001B[2m[path]\u001B[0m\n\n" +
	"\u001B[1mArguments:\u001B[0m\n" +
	"  path - Path to root directory, current dir if not provided.\n"

func init() {
	flag.Usage = func() {
		_, _ = fmt.Fprint(flag.CommandLine.Output(), help)
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	path := "."
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		// virtually impossible, but
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	println("Validating resources at", path)

	if err := lint.Validate(path); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	println("OK")
}
