package cli

import (
	"fmt"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/maneuver"
	"DebrisLedger/internal/mission"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/rank"
	"DebrisLedger/internal/report"
	"DebrisLedger/internal/screen"
	"DebrisLedger/internal/store"
)

// validateOutput is the JSON form of the validate subcommand.
type validateOutput struct {
	Status         string   `json:"status"`
	ScenarioLabel  string   `json:"scenario_label"`
	ConfigLabel    string   `json:"config_label"`
	ConfigDigest   string   `json:"config_digest"`
	EpochSeconds   int64    `json:"epoch_s"`
	HorizonSeconds int64    `json:"horizon_s"`
	Objects        int      `json:"objects"`
	Assets         int      `json:"assets"`
	Chasers        int      `json:"chasers"`
	Volumes        int      `json:"screening_volumes"`
	ObjectIDs      []string `json:"object_ids"`
	AssetIDs       []string `json:"asset_ids"`
	ChaserIDs      []string `json:"chaser_ids"`
}

func runValidate(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("validate", env.Stderr, opts, "config", "scenario", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	sc, err := loadScenarioFile(opts.Scenario)
	if err != nil {
		return err
	}
	out := validateOutput{
		Status:         "ok",
		ScenarioLabel:  sc.Label,
		ConfigLabel:    cfg.Label,
		ConfigDigest:   cfg.Summary(),
		EpochSeconds:   sc.EpochSeconds,
		HorizonSeconds: sc.HorizonSeconds,
		Objects:        len(sc.Objects),
		Assets:         len(sc.Assets),
		Chasers:        len(sc.Chasers),
		Volumes:        len(sc.ScreeningVolumes),
		ObjectIDs:      objectIDs(sc),
		AssetIDs:       assetIDs(sc),
		ChaserIDs:      chaserIDs(sc),
	}
	progress(env, opts, "validated %d object(s), %d asset(s), %d chaser(s)", out.Objects, out.Assets, out.Chasers)
	return render(env, opts, report.ValidationText(sc, cfg), out)
}

// ingestOutput is the JSON form of the ingest subcommand.
type ingestOutput struct {
	Status        string     `json:"status"`
	Store         string     `json:"store"`
	ScenarioLabel string     `json:"scenario_label"`
	Records       int        `json:"records"`
	AuditSequence int        `json:"audit_sequence"`
	AuditHash     string     `json:"audit_hash"`
	Meta          store.Meta `json:"meta"`
}

func runIngest(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("ingest", env.Stderr, opts, "config", "scenario", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	sc, err := loadScenarioFile(opts.Scenario)
	if err != nil {
		return err
	}
	st, err := openStore(opts)
	if err != nil {
		return err
	}
	entry, err := st.Ingest(sc, cfg.Label)
	if err != nil {
		return err
	}
	meta, err := st.Meta()
	if err != nil {
		return err
	}
	out := ingestOutput{
		Status:        "ok",
		Store:         st.Root,
		ScenarioLabel: sc.Label,
		Records:       meta.Counts.Records,
		AuditSequence: entry.Sequence,
		AuditHash:     entry.Hash,
		Meta:          meta,
	}
	progress(env, opts, "ingested %q into %s (%d records)", sc.Label, st.Root, meta.Counts.Records)
	text := fmt.Sprintf("%s\n\ningest\n------\nstore:      %s\nscenario:   %s\nrecords:    %d\naudit seq:  %d\naudit hash: %s\nobjects:    %d\nassets:     %d\nchasers:    %d\nvolumes:    %d\n",
		report.Banner, st.Root, sc.Label, meta.Counts.Records, entry.Sequence, entry.Hash,
		meta.Counts.Objects, meta.Counts.Assets, meta.Counts.Chasers, meta.Counts.Volumes)
	return render(env, opts, text, out)
}

func runScreen(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("screen", env.Stderr, opts, "config", "scenario", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, st, sc, err := stageInputs(opts)
	if err != nil {
		return err
	}
	res, err := screen.Run(sc, cfg)
	if err != nil {
		return err
	}
	entry, err := st.SaveSnapshot(store.SnapshotScreening, sc.EpochSeconds, res,
		fmt.Sprintf("%d conjunction(s), %d actionable", res.Stats.ConjunctionCount, res.Stats.ActionableCount))
	if err != nil {
		return err
	}
	progress(env, opts, "screened %d pair(s): %d conjunction(s), audit seq %d",
		res.Stats.PairsScreened, res.Stats.ConjunctionCount, entry.Sequence)
	return render(env, opts, report.ScreeningText(res, cfg), res)
}

func runManeuver(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("maneuver", env.Stderr, opts, "config", "scenario", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, st, sc, err := stageInputs(opts)
	if err != nil {
		return err
	}
	var res screen.Result
	if err := st.LoadSnapshot(store.SnapshotScreening, &res); err != nil {
		return fmt.Errorf("%w; run the screen subcommand first", err)
	}
	plan := maneuver.Build(sc, cfg, res)
	entry, err := st.SaveSnapshot(store.SnapshotManeuver, sc.EpochSeconds, plan,
		fmt.Sprintf("%d scheduled, %d rejected", plan.Stats.Scheduled, plan.Stats.Rejected))
	if err != nil {
		return err
	}
	progress(env, opts, "planned %d burn(s) totalling %.5f m/s, audit seq %d",
		plan.Stats.Scheduled, plan.Stats.TotalDeltaVMps, entry.Sequence)
	return render(env, opts, report.ManeuverText(plan, cfg), plan)
}

func runRank(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("rank", env.Stderr, opts, "config", "scenario", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, st, sc, err := stageInputs(opts)
	if err != nil {
		return err
	}
	var res screen.Result
	if err := st.LoadSnapshot(store.SnapshotScreening, &res); err != nil {
		return fmt.Errorf("%w; run the screen subcommand first", err)
	}
	ranking := rank.Build(sc, cfg, res)
	entry, err := st.SaveSnapshot(store.SnapshotRanking, sc.EpochSeconds, ranking,
		fmt.Sprintf("%d target(s), %d feasible", len(ranking.Targets), ranking.Stats.Feasible))
	if err != nil {
		return err
	}
	progress(env, opts, "ranked %d target(s), %d feasible, audit seq %d",
		len(ranking.Targets), ranking.Stats.Feasible, entry.Sequence)
	return render(env, opts, report.RankingText(ranking, cfg), ranking)
}

func runMission(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("mission", env.Stderr, opts, "config", "scenario", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, st, sc, err := stageInputs(opts)
	if err != nil {
		return err
	}
	var ranking rank.Ranking
	if err := st.LoadSnapshot(store.SnapshotRanking, &ranking); err != nil {
		return fmt.Errorf("%w; run the rank subcommand first", err)
	}
	plan := mission.Build(sc, cfg, ranking)
	entry, err := st.SaveSnapshot(store.SnapshotMission, sc.EpochSeconds, plan,
		fmt.Sprintf("%d mission(s), %d removal(s)", len(plan.Missions), len(plan.RemovedIDs())))
	if err != nil {
		return err
	}
	progress(env, opts, "planned %d mission(s) removing %d target(s), audit seq %d",
		len(plan.Missions), len(plan.RemovedIDs()), entry.Sequence)
	return render(env, opts, report.MissionText(plan, cfg), plan)
}

func runVerify(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("verify", env.Stderr, opts, "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	st, err := openStore(opts)
	if err != nil {
		return err
	}
	v, err := st.Verify()
	if err != nil {
		return err
	}
	progress(env, opts, "verified %d audit entry(ies) and %d snapshot(s)", v.Audit.Entries, len(v.Snapshots))
	if err := render(env, opts, report.VerifyText(v), v); err != nil {
		return err
	}
	if !v.OK {
		return fmt.Errorf("store verification failed with %d finding(s)", len(v.Findings))
	}
	return nil
}

func runReport(env *Env, args []string) error {
	opts := &options{}
	fs := newFlagSet("report", env.Stderr, opts, "config", "store", "format", "out")
	if err := parse(fs, args, opts); err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	st, err := openStore(opts)
	if err != nil {
		return err
	}
	meta, err := st.Meta()
	if err != nil {
		return err
	}
	var (
		screening *screen.Result
		avoidance *maneuver.Plan
		ranking   *rank.Ranking
		campaign  *mission.Plan
	)
	if st.HasSnapshot(store.SnapshotScreening) {
		var res screen.Result
		if err := st.LoadSnapshot(store.SnapshotScreening, &res); err != nil {
			return err
		}
		screening = &res
	}
	if st.HasSnapshot(store.SnapshotManeuver) {
		var plan maneuver.Plan
		if err := st.LoadSnapshot(store.SnapshotManeuver, &plan); err != nil {
			return err
		}
		avoidance = &plan
	}
	if st.HasSnapshot(store.SnapshotRanking) {
		var r rank.Ranking
		if err := st.LoadSnapshot(store.SnapshotRanking, &r); err != nil {
			return err
		}
		ranking = &r
	}
	if st.HasSnapshot(store.SnapshotMission) {
		var plan mission.Plan
		if err := st.LoadSnapshot(store.SnapshotMission, &plan); err != nil {
			return err
		}
		campaign = &plan
	}
	v, err := st.Verify()
	if err != nil {
		return err
	}
	combined := report.BuildCombined(cfg, meta, screening, avoidance, ranking, campaign, &v)
	if _, err := st.SaveSnapshot(store.SnapshotReport, meta.EpochSeconds, combined,
		fmt.Sprintf("%d conjunction(s), %d removal(s)", combined.Summary.Conjunctions, combined.Summary.RemovedTargets)); err != nil {
		return err
	}
	progress(env, opts, "reported %d conjunction(s) and %d removal(s)",
		combined.Summary.Conjunctions, combined.Summary.RemovedTargets)
	return render(env, opts, report.CombinedText(combined, cfg), combined)
}

// stageInputs is the shared preamble of the pipeline subcommands.
func stageInputs(opts *options) (config.Config, *store.Store, *model.Scenario, error) {
	cfg, err := loadConfig(opts)
	if err != nil {
		return config.Config{}, nil, nil, err
	}
	st, err := openStore(opts)
	if err != nil {
		return config.Config{}, nil, nil, err
	}
	sc, err := resolveScenario(opts, st)
	if err != nil {
		return config.Config{}, nil, nil, err
	}
	return cfg, st, sc, nil
}

func objectIDs(sc *model.Scenario) []string {
	out := make([]string, 0, len(sc.Objects))
	for _, obj := range sc.Objects {
		out = append(out, obj.ID)
	}
	return out
}

func assetIDs(sc *model.Scenario) []string {
	out := make([]string, 0, len(sc.Assets))
	for _, a := range sc.Assets {
		out = append(out, a.ID)
	}
	return out
}

func chaserIDs(sc *model.Scenario) []string {
	out := make([]string, 0, len(sc.Chasers))
	for _, c := range sc.Chasers {
		out = append(out, c.ID)
	}
	return out
}
