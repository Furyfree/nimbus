// Command nimbus is the Nimbus engine entry point.
package main

import (
	"os"

	"github.com/Furyfree/nimbus/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
