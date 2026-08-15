// Command debrisledger is the offline CLI for conjunction screening and
// active debris-removal mission planning.
//
// The binary is fully offline: it never opens a network connection, never reads
// the wall clock for stored output and depends only on the Go standard library.
package main

import (
	"os"

	"DebrisLedger/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
