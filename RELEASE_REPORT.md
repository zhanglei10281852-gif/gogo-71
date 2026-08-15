# DebrisLedger release report

Independent original project. No affiliation, endorsement, sponsorship or
association with any other product, company, agency or organization. All bundled
data is fictional. The orbital model is a deliberately simplified circular-orbit
approximation and is not suitable for operational spaceflight decisions.

## Module

| item                  | value                           |
| --------------------- | ------------------------------- |
| module path           | `DebrisLedger`                  |
| declared Go version   | `go 1.22.5`                     |
| external dependencies | none (Go standard library only) |
| binary                | `cmd/debrisledger`              |
| CLI version string    | `1.0.0`                         |

## Effective production LOC

Counting rules: `.go` files only, excluding `*_test.go`, blank lines and
comment-only lines (line comments and block-comment interiors). No generated
files exist in this repository.

| file                              | effective LOC |
| --------------------------------- | ------------- |
| cmd/debrisledger/main.go          | 8             |
| internal/cli/cli.go               | 103           |
| internal/cli/commands.go          | 328           |
| internal/cli/flags.go             | 125           |
| internal/compliance/atmosphere.go | 111           |
| internal/compliance/rules.go      | 259           |
| internal/config/config.go         | 411           |
| internal/maneuver/plan.go         | 332           |
| internal/mission/mission.go       | 423           |
| internal/model/types.go           | 186           |
| internal/model/validate.go        | 275           |
| internal/numeric/numeric.go       | 154           |
| internal/orbit/approach.go        | 257           |
| internal/orbit/frame.go           | 44            |
| internal/orbit/kepler.go          | 145           |
| internal/orbit/transfer.go        | 132           |
| internal/orbit/vector.go          | 84            |
| internal/rank/rank.go             | 270           |
| internal/report/combined.go       | 190           |
| internal/report/table.go          | 130           |
| internal/report/text.go           | 408           |
| internal/screen/probability.go    | 73            |
| internal/screen/screen.go         | 274           |
| internal/store/atomic.go          | 115           |
| internal/store/audit.go           | 133           |
| internal/store/store.go           | 599           |
| internal/strictjson/strictjson.go | 145           |
| **total**                         | **5714**      |

Requirement was at least 2600 effective production LOC.

Tests are additional and not counted above: 14 test files containing 176 test
functions.

## Packages

| package                   | responsibility                                                          |
| ------------------------- | ----------------------------------------------------------------------- |
| `main` (cmd/debrisledger) | process entry point, exit-code plumbing                                 |
| `internal/cli`            | subcommand table, flag parsing, output selection, stage ordering        |
| `internal/config`         | strict configuration, defaults, cross-field validation                  |
| `internal/model`          | domain types, per-entity and whole-scenario validation                  |
| `internal/strictjson`     | the only JSON entry points; strict decode, deterministic encode         |
| `internal/numeric`        | rounding, fixed-notation formatting, order-independent sums             |
| `internal/orbit`          | circular-orbit mechanics, RIC frame, closest-approach search, transfers |
| `internal/screen`         | pairwise conjunction screening, probability model, severity, congestion |
| `internal/maneuver`       | along-track avoidance planning, budget, allowance, conflict detection   |
| `internal/rank`           | removal-target risk scoring and capture feasibility                     |
| `internal/mission`        | chaser assignment, transfer costing, disposal selection, timeline       |
| `internal/compliance`     | atmosphere and lifetime estimate, disposal rules, residual risk         |
| `internal/report`         | deterministic text tables and JSON artefacts                            |
| `internal/store`          | append-only JSONL ledgers, atomic snapshots, hash-chained audit log     |

14 packages in total (1 command, 13 internal libraries).

## Validation

All commands were run from the repository root with `GOTOOLCHAIN=local` and
`GOPROXY=off`, with `GOCACHE` and `GOTMPDIR` redirected under the ignored
`.cache/` directory.

| command                  | result                            |
| ------------------------ | --------------------------------- |
| `gofmt -l .`             | no output (clean)                 |
| `go build ./...`         | exit 0                            |
| `go vet ./...`           | exit 0                            |
| `go test ./... -count=1` | exit 0, all 13 test packages `ok` |

Test package results from the final run:

```
?       DebrisLedger/cmd/debrisledger   [no test files]
ok      DebrisLedger/internal/cli
ok      DebrisLedger/internal/compliance
ok      DebrisLedger/internal/config
ok      DebrisLedger/internal/maneuver
ok      DebrisLedger/internal/mission
ok      DebrisLedger/internal/model
ok      DebrisLedger/internal/numeric
ok      DebrisLedger/internal/orbit
ok      DebrisLedger/internal/rank
ok      DebrisLedger/internal/report
ok      DebrisLedger/internal/screen
ok      DebrisLedger/internal/store
ok      DebrisLedger/internal/strictjson
```

Numerical coverage highlights:

- the closest-approach search is checked against an analytic leader/follower
  geometry whose time of closest approach and miss distance are known in closed
  form (agreement within 0.05 s and 1e-6 km);
- the refined result is checked against a brute-force 0.01 s scan of the same
  window and must never be worse;
- the answer is checked to be insensitive to the coarse grid step (10, 20, 25 and
  60 s all agree within 0.2 s) and to be bit-identical across repeated runs;
- golden-section minimisation is checked against an analytic quadratic;
- Hohmann, plane-change and deorbit costs are checked against known reference
  values (300 km to 35786 km is about 3900 m/s; a 60 degree plane change equals
  the orbital speed).

## Offline CLI smoke workflow

Run on the host against `examples/`, in pipeline order. All stages exited 0:

```
validate=0 ingest=0 screen=0 maneuver=0 rank=0 mission=0 verify=0 report=0
```

Resulting store census (`meta.json`): 22 catalogue objects, 3 assets, 2 chasers,
2 screening volumes, 30 append-only catalogue records, 6 ledger events, 5
snapshots, 6 audit entries, audit chain verified.

Stage outcomes with the bundled fictional data:

- screening: 66 pairs screened, 9 conjunctions retained (7 high, 1 moderate,
  1 low), 7 actionable, minimum miss distance 0.3589 km, maximum probability
  8.356020e-05;
- avoidance: 7 candidates, 5 scheduled for 0.2445 m/s total, 1 rejected as a
  window conflict, 1 rejected for insufficient lead time, summed probability
  reduced from 3.416410e-04 to 1.325060e-04;
- ranking: 20 debris-class objects scored, 2 payloads excluded, 16 listed under
  the configured cap, 8 capturable;
- mission: 2 chasers, 3 targets removed (2 controlled reentries, 1 object left
  to natural decay), catalogue risk reduced by 29.208 percent, both chasers
  inside budget, unaffordable and un-capturable targets reported with reasons;
- verify: audit chain, snapshot hashes, metadata head and catalogue replay all
  pass.

## Docker

`Dockerfile` at the repository root, two stages:

- builder `FROM golang:1.22` with `GOTOOLCHAIN=local`, `CGO_ENABLED=0`,
  `GOPROXY=off`, running `go vet ./...` and then building the CLI;
- final `FROM scratch` containing only the static binary, with
  `ENTRYPOINT ["/debrisledger"]`.

| step                                                                               | result                                                            |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| `docker build -t debrisledger:local .`                                             | success (builder vet + build passed inside the image)             |
| `docker run --rm --network none debrisledger:local --version`                      | `DebrisLedger 1.0.0`, exit 0                                      |
| `docker run --rm --network none debrisledger:local --help`                         | usage printed, exit 0                                             |
| full pipeline in-container with a bind-mounted work directory and `--network none` | `ingest=0 screen=0 maneuver=0 rank=0 mission=0 verify=0 report=0` |

Cross-platform determinism check: the `screening.json` snapshot produced by the
Windows host and by the Linux `scratch` container are byte-identical
(`sha256:b48905730c6427082c14e1a91f8768499dbb6b5cc91f587fa38ec7c3d2174100`).

## Determinism guarantees exercised

- no wall-clock reads and no randomness anywhere in the codebase;
- all iteration over maps is funnelled through sorted key slices;
- floats are rounded before storage and formatted with `strconv` in fixed
  notation; probabilities are rounded to a fixed number of significant digits;
- floating-point sums are taken in sorted-magnitude order;
- a CLI test runs the whole pipeline twice into two different stores and asserts
  that the screening, avoidance, ranking, mission and audit artefacts are
  byte-identical.

The single intentional exception is the absolute store path recorded in the
verification block of the campaign report.

## Artefact inventory

| path                                             | purpose                                                                 |
| ------------------------------------------------ | ----------------------------------------------------------------------- |
| `go.mod`                                         | module `DebrisLedger`, `go 1.22.5`, no requires                         |
| `Dockerfile`, `.dockerignore`                    | multi-stage static build, scratch runtime                               |
| `README.md`                                      | independence notice, model caveats, usage, formats, determinism, Docker |
| `RELEASE_REPORT.md`                              | this report                                                             |
| `.gitignore`                                     | ignores `.cache/` and local binaries                                    |
| `examples/config.json`, `examples/scenario.json` | fictional sample inputs                                                 |
| `cmd/`, `internal/`                              | production code and tests                                               |
