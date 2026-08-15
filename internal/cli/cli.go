// Package cli implements the DebrisLedger command-line interface.
//
// Every subcommand is offline: it reads local files, writes local files and
// never opens a network connection. Output is either deterministic text or
// deterministic JSON, selected with --format.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Version is the tool version reported by --version.
const Version = "1.0.0"

// Command is one subcommand.
type Command struct {
	Name    string
	Summary string
	Run     func(env *Env, args []string) error
}

// Env carries the streams a command writes to. Keeping them in a struct makes
// the whole CLI testable without touching the process streams.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Commands returns the subcommand table in execution order, which is also the
// order the pipeline is meant to be run in.
func Commands() []Command {
	return []Command{
		{Name: "validate", Summary: "strictly decode a config and scenario and report the parsed model", Run: runValidate},
		{Name: "ingest", Summary: "append a scenario to the append-only local store", Run: runIngest},
		{Name: "screen", Summary: "screen assets against the catalogue and store the conjunction set", Run: runScreen},
		{Name: "maneuver", Summary: "plan collision-avoidance burns for actionable conjunctions", Run: runManeuver},
		{Name: "rank", Summary: "score and rank debris-removal targets", Run: runRank},
		{Name: "mission", Summary: "plan removal missions, disposal actions and the timeline", Run: runMission},
		{Name: "verify", Summary: "verify the hash-chained audit log and snapshot hashes", Run: runVerify},
		{Name: "report", Summary: "render the cross-stage campaign report", Run: runReport},
	}
}

// Run dispatches a command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	env := &Env{Stdout: stdout, Stderr: stderr}
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		writeUsage(stdout)
		return 0
	case "--version", "version":
		fmt.Fprintf(stdout, "%s %s\n", "DebrisLedger", Version)
		return 0
	}
	for _, cmd := range Commands() {
		if cmd.Name != args[0] {
			continue
		}
		if err := cmd.Run(env, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintf(stderr, "debrisledger %s: %v\n", cmd.Name, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "unknown subcommand %q\n\n", args[0])
	writeUsage(stderr)
	return 2
}

func writeUsage(w io.Writer) {
	fmt.Fprintf(w, "DebrisLedger %s - offline conjunction screening and debris-removal planning\n\n", Version)
	fmt.Fprintf(w, "usage: debrisledger <command> [flags]\n\n")
	fmt.Fprintf(w, "commands:\n")
	cmds := Commands()
	width := 0
	for _, c := range cmds {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.Name, c.Summary)
	}
	fmt.Fprintf(w, "\ncommon flags:\n")
	for _, line := range []string{
		"--config PATH      strict JSON configuration file (defaults are used when omitted)",
		"--scenario PATH    strict JSON scenario file",
		"--store PATH       local store directory",
		"--format FORMAT    text (default) or json",
		"--out PATH         write the rendered output to a file instead of stdout",
	} {
		fmt.Fprintf(w, "  %s\n", line)
	}
	fmt.Fprintf(w, "\npipeline: validate -> ingest -> screen -> maneuver -> rank -> mission -> verify -> report\n")
	fmt.Fprintf(w, "\nThe orbital model is a deliberately simplified circular-orbit approximation.\n")
	fmt.Fprintf(w, "It is not suitable for operational spaceflight decisions.\n")
}

// formatNames lists the accepted --format values.
func formatNames() []string {
	out := []string{"json", "text"}
	sort.Strings(out)
	return out
}

func checkFormat(format string) error {
	for _, name := range formatNames() {
		if format == name {
			return nil
		}
	}
	return fmt.Errorf("unknown --format %q (accepted: %s)", format, strings.Join(formatNames(), ", "))
}
