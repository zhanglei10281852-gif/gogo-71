package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/store"
	"DebrisLedger/internal/strictjson"
)

// options holds the flag values shared by the subcommands.
type options struct {
	Config   string
	Scenario string
	Store    string
	Format   string
	Out      string
	Quiet    bool
}

// newFlagSet builds a flag set that reports errors through the command's error
// return instead of exiting the process.
func newFlagSet(name string, stderr io.Writer, opts *options, wanted ...string) *flag.FlagSet {
	fs := flag.NewFlagSet("debrisledger "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	want := map[string]bool{}
	for _, w := range wanted {
		want[w] = true
	}
	if want["config"] {
		fs.StringVar(&opts.Config, "config", "", "strict JSON configuration file")
	}
	if want["scenario"] {
		fs.StringVar(&opts.Scenario, "scenario", "", "strict JSON scenario file")
	}
	if want["store"] {
		fs.StringVar(&opts.Store, "store", "", "local store directory")
	}
	if want["format"] {
		fs.StringVar(&opts.Format, "format", "text", "output format: text or json")
	}
	if want["out"] {
		fs.StringVar(&opts.Out, "out", "", "write output to this file instead of stdout")
	}
	fs.BoolVar(&opts.Quiet, "quiet", false, "suppress the progress line on stderr")
	return fs
}

// parse parses args and rejects positional leftovers, which usually mean a
// mistyped flag.
func parse(fs *flag.FlagSet, args []string, opts *options) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional argument %q", fs.Arg(0))
	}
	if opts.Format != "" {
		if err := checkFormat(opts.Format); err != nil {
			return err
		}
	}
	return nil
}

// loadConfig loads the configuration, falling back to the built-in defaults.
func loadConfig(opts *options) (config.Config, error) {
	cfg, err := config.Load(opts.Config)
	if err != nil {
		return config.Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("configuration: %w", err)
	}
	return cfg, nil
}

// loadScenarioFile strictly decodes and validates a scenario file.
func loadScenarioFile(path string) (*model.Scenario, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("--scenario is required")
	}
	var sc model.Scenario
	if err := strictjson.DecodeFile(path, &sc); err != nil {
		return nil, err
	}
	sc.Sort()
	if err := sc.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &sc, nil
}

// openStore opens the local store named by --store.
func openStore(opts *options) (*store.Store, error) {
	if strings.TrimSpace(opts.Store) == "" {
		return nil, fmt.Errorf("--store is required")
	}
	return store.Open(opts.Store)
}

// resolveScenario prefers an explicit --scenario file and otherwise replays the
// store catalogue.
func resolveScenario(opts *options, st *store.Store) (*model.Scenario, error) {
	if strings.TrimSpace(opts.Scenario) != "" {
		return loadScenarioFile(opts.Scenario)
	}
	return st.Scenario()
}

// emit writes the rendered artefact to --out or to stdout.
func emit(env *Env, opts *options, text string) error {
	if strings.TrimSpace(opts.Out) == "" {
		if _, err := io.WriteString(env.Stdout, text); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}
	if err := store.WriteFileAtomic(opts.Out, []byte(text)); err != nil {
		return err
	}
	if !opts.Quiet {
		fmt.Fprintf(env.Stderr, "wrote %s (%d bytes)\n", opts.Out, len(text))
	}
	return nil
}

// render picks the text or JSON representation of a stage result.
func render(env *Env, opts *options, textForm string, jsonForm any) error {
	if opts.Format == "json" {
		data, err := strictjson.Marshal(jsonForm)
		if err != nil {
			return err
		}
		return emit(env, opts, string(data))
	}
	return emit(env, opts, textForm)
}

// progress writes a one-line status to stderr, which keeps stdout clean for
// piping. It is suppressed by --quiet and never appears in stored artefacts.
func progress(env *Env, opts *options, format string, args ...any) {
	if opts.Quiet {
		return
	}
	fmt.Fprintf(env.Stderr, format+"\n", args...)
}
