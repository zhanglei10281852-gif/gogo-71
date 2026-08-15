// Package screen performs conjunction screening: it pairs every operator asset
// against every catalogue object, finds the closest approach inside the
// screening horizon, and classifies the encounter.
package screen

import (
	"fmt"
	"sort"

	"DebrisLedger/internal/config"
	"DebrisLedger/internal/model"
	"DebrisLedger/internal/numeric"
	"DebrisLedger/internal/orbit"
)

// Diagnostics records how much work the closest-approach search needed. It is
// stored so that a report can show that the refinement actually bracketed a
// minimum instead of returning a grid sample.
type Diagnostics struct {
	CoarseSamples    int  `json:"coarse_samples"`
	RefineIterations int  `json:"refine_iterations"`
	Candidates       int  `json:"candidates"`
	Bracketed        bool `json:"bracketed"`
}

// Conjunction is one screened close approach.
type Conjunction struct {
	AssetID         string            `json:"asset_id"`
	AssetName       string            `json:"asset_name"`
	ObjectID        string            `json:"object_id"`
	ObjectName      string            `json:"object_name"`
	ObjectClass     model.ObjectClass `json:"object_class"`
	TCASeconds      int64             `json:"tca_s"`
	TCAFractional   float64           `json:"tca_fractional_s"`
	LeadSeconds     int64             `json:"lead_s"`
	MissKm          float64           `json:"miss_km"`
	RadialKm        float64           `json:"radial_km"`
	InTrackKm       float64           `json:"in_track_km"`
	CrossTrackKm    float64           `json:"cross_track_km"`
	RelSpeedKmS     float64           `json:"rel_speed_km_s"`
	RangeRateKmS    float64           `json:"range_rate_km_s"`
	CombinedRadiusM float64           `json:"combined_radius_m"`
	SigmaKm         float64           `json:"sigma_km"`
	Probability     float64           `json:"probability"`
	Severity        string            `json:"severity"`
	SeverityRank    int               `json:"severity_rank"`
	Actionable      bool              `json:"actionable"`
	VolumeName      string            `json:"volume_name"`
	InsideVolume    bool              `json:"inside_volume"`
	Diagnostics     Diagnostics       `json:"diagnostics"`
}

// Key is the stable identity of a conjunction, used for sorting and for
// cross-referencing between artefacts.
func (c Conjunction) Key() string {
	return c.AssetID + "|" + c.ObjectID + "|" + fmt.Sprintf("%d", c.TCASeconds)
}

// SeverityCount is one row of the severity histogram.
type SeverityCount struct {
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

// Stats summarises a screening run.
type Stats struct {
	PairsScreened    int             `json:"pairs_screened"`
	PairsSkipped     int             `json:"pairs_skipped"`
	SearchFailures   int             `json:"search_failures"`
	ConjunctionCount int             `json:"conjunction_count"`
	ActionableCount  int             `json:"actionable_count"`
	TruncatedAssets  []string        `json:"truncated_assets"`
	BySeverity       []SeverityCount `json:"by_severity"`
	MaxProbability   float64         `json:"max_probability"`
	MinMissKm        float64         `json:"min_miss_km"`
}

// Result is the complete output of a screening run.
type Result struct {
	ScenarioLabel  string        `json:"scenario_label"`
	ConfigLabel    string        `json:"config_label"`
	EpochSeconds   int64         `json:"epoch_s"`
	HorizonSeconds int64         `json:"horizon_s"`
	Conjunctions   []Conjunction `json:"conjunctions"`
	Stats          Stats         `json:"stats"`
	Assumptions    []string      `json:"assumptions"`
}

// ByAsset groups conjunctions by asset identifier, preserving the stored order
// inside each group.
func (r Result) ByAsset() map[string][]Conjunction {
	out := map[string][]Conjunction{}
	for _, c := range r.Conjunctions {
		out[c.AssetID] = append(out[c.AssetID], c)
	}
	return out
}

// AssetIDs returns the sorted list of assets that have at least one
// conjunction.
func (r Result) AssetIDs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range r.Conjunctions {
		if !seen[c.AssetID] {
			seen[c.AssetID] = true
			out = append(out, c.AssetID)
		}
	}
	sort.Strings(out)
	return out
}

// ProbabilityByObject sums the probability contribution of every asset against
// each catalogue object, which is the input the ranking stage uses.
func (r Result) ProbabilityByObject() map[string]float64 {
	buckets := map[string][]float64{}
	for _, c := range r.Conjunctions {
		buckets[c.ObjectID] = append(buckets[c.ObjectID], c.Probability)
	}
	out := make(map[string]float64, len(buckets))
	for id, values := range buckets {
		out[id] = numeric.SumFloats(values)
	}
	return out
}

// Run screens the scenario. The returned error is non-nil only for
// configuration problems; individual search failures are counted in Stats so
// that one pathological pair cannot abort a whole run.
func Run(sc *model.Scenario, cfg config.Config) (Result, error) {
	if len(sc.Assets) == 0 {
		return Result{}, fmt.Errorf("scenario %q declares no assets to screen", sc.Label)
	}
	prop := orbit.NewPropagator(cfg.Propagation.IncludeJ2NodalDrift)
	skip := cfg.SkipClassSet()
	res := Result{
		ScenarioLabel:  sc.Label,
		ConfigLabel:    cfg.Label,
		EpochSeconds:   sc.EpochSeconds,
		HorizonSeconds: sc.HorizonSeconds,
		Assumptions:    ProbabilityAssumptions(),
	}
	stats := Stats{MinMissKm: -1}
	perAsset := map[string][]Conjunction{}

	for _, asset := range sc.Assets {
		for _, obj := range sc.Objects {
			if obj.ID == asset.ID || skip[string(obj.Class)] {
				stats.PairsSkipped++
				continue
			}
			stats.PairsScreened++
			c, ok, err := screenPair(prop, sc, cfg, asset, obj)
			if err != nil {
				stats.SearchFailures++
				continue
			}
			if !ok {
				continue
			}
			perAsset[asset.ID] = append(perAsset[asset.ID], c)
		}
	}

	assetIDs := make([]string, 0, len(perAsset))
	for id := range perAsset {
		assetIDs = append(assetIDs, id)
	}
	sort.Strings(assetIDs)
	for _, id := range assetIDs {
		list := perAsset[id]
		sortConjunctions(list)
		if len(list) > cfg.Screening.MaxConjunctionsPerAsset {
			list = list[:cfg.Screening.MaxConjunctionsPerAsset]
			stats.TruncatedAssets = append(stats.TruncatedAssets, id)
		}
		res.Conjunctions = append(res.Conjunctions, list...)
	}
	sortConjunctions(res.Conjunctions)

	for _, c := range res.Conjunctions {
		stats.ConjunctionCount++
		if c.Actionable {
			stats.ActionableCount++
		}
		if c.Probability > stats.MaxProbability {
			stats.MaxProbability = c.Probability
		}
		if stats.MinMissKm < 0 || c.MissKm < stats.MinMissKm {
			stats.MinMissKm = c.MissKm
		}
	}
	if stats.MinMissKm < 0 {
		stats.MinMissKm = 0
	}
	stats.BySeverity = histogram(res.Conjunctions)
	sort.Strings(stats.TruncatedAssets)
	res.Stats = stats
	return res, nil
}

func screenPair(prop orbit.Propagator, sc *model.Scenario, cfg config.Config,
	asset model.Asset, obj model.CatalogObject) (Conjunction, bool, error) {
	opts := orbit.SearchOptions{
		StartS:        float64(sc.EpochSeconds),
		EndS:          float64(sc.EndSeconds()),
		CoarseStepS:   cfg.Propagation.CoarseStepSeconds,
		ToleranceS:    cfg.Propagation.RefineToleranceS,
		MaxIterations: cfg.Propagation.MaxRefineIterations,
	}
	app, err := orbit.ClosestApproach(prop, asset.Orbit, obj.Orbit, opts)
	if err != nil {
		return Conjunction{}, false, fmt.Errorf("asset %s vs object %s: %w", asset.ID, obj.ID, err)
	}
	volume := sc.VolumeFor(obj.Class)
	inside := orbit.InsideVolume(app, volume)
	if app.MissKm > cfg.Screening.MissDistanceThresholdKm && !inside {
		return Conjunction{}, false, nil
	}
	lead := app.TimeS - float64(sc.EpochSeconds)
	sigma := PositionSigmaKm(cfg.Probability, lead)
	radius := CombinedHardBodyRadiusM(asset.AreaM2, obj.AreaM2, cfg.Probability.HardBodyMarginM)
	probability := CollisionProbability(app.MissKm, radius, sigma)
	severity := Classify(cfg.Severity, probability, app.MissKm)
	dd := cfg.Output.DistanceDecimals

	c := Conjunction{
		AssetID:         asset.ID,
		AssetName:       asset.Name,
		ObjectID:        obj.ID,
		ObjectName:      obj.Name,
		ObjectClass:     obj.Class,
		TCASeconds:      numeric.SecondsToWholeSeconds(app.TimeS),
		TCAFractional:   numeric.Round(app.TimeS, 3),
		LeadSeconds:     numeric.SecondsToWholeSeconds(lead),
		MissKm:          numeric.Round(app.MissKm, dd),
		RadialKm:        numeric.Round(app.RadialKm, dd),
		InTrackKm:       numeric.Round(app.InTrackKm, dd),
		CrossTrackKm:    numeric.Round(app.CrossTrackKm, dd),
		RelSpeedKmS:     numeric.Round(app.RelSpeedKmS, 6),
		RangeRateKmS:    numeric.Round(app.RangeRateKmS, 6),
		CombinedRadiusM: numeric.Round(radius, 3),
		SigmaKm:         numeric.Round(sigma, 6),
		Probability:     numeric.RoundSignificant(probability, cfg.Output.ProbabilityExponentDigits),
		Severity:        severity,
		SeverityRank:    config.SeverityRank(severity),
		VolumeName:      volume.Name,
		InsideVolume:    inside,
		Diagnostics: Diagnostics{
			CoarseSamples:    app.CoarseSamples,
			RefineIterations: app.RefineIterations,
			Candidates:       app.Candidates,
			Bracketed:        app.Bracketed,
		},
	}
	c.Actionable = c.SeverityRank >= cfg.ActionableRank()
	return c, true, nil
}

// sortConjunctions imposes the canonical ordering: most severe first, then
// smallest miss distance, then earliest closest approach, then identifiers.
func sortConjunctions(list []Conjunction) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.SeverityRank != b.SeverityRank {
			return a.SeverityRank > b.SeverityRank
		}
		if a.MissKm != b.MissKm {
			return a.MissKm < b.MissKm
		}
		if a.TCASeconds != b.TCASeconds {
			return a.TCASeconds < b.TCASeconds
		}
		if a.AssetID != b.AssetID {
			return a.AssetID < b.AssetID
		}
		return a.ObjectID < b.ObjectID
	})
}

func histogram(list []Conjunction) []SeverityCount {
	counts := map[string]int{}
	for _, c := range list {
		counts[c.Severity]++
	}
	out := make([]SeverityCount, 0, len(config.SeverityLabels()))
	for _, label := range config.SeverityLabels() {
		out = append(out, SeverityCount{Severity: label, Count: counts[label]})
	}
	return out
}

// Congestion counts how many catalogue objects share an altitude and
// inclination neighbourhood with each object. The result feeds the removal
// ranking stage and is computed here because it is a property of the screened
// catalogue rather than of a single object.
func Congestion(sc *model.Scenario, cfg config.RankingConfig) map[string]int {
	out := make(map[string]int, len(sc.Objects))
	for _, a := range sc.Objects {
		count := 0
		for _, b := range sc.Objects {
			if a.ID == b.ID {
				continue
			}
			dAlt := a.Orbit.AltitudeKm - b.Orbit.AltitudeKm
			if dAlt < 0 {
				dAlt = -dAlt
			}
			dInc := a.Orbit.InclinationDeg - b.Orbit.InclinationDeg
			if dInc < 0 {
				dInc = -dInc
			}
			if dAlt <= cfg.CongestionAltitudeBandKm && dInc <= cfg.CongestionInclinationBandDeg {
				count++
			}
		}
		out[a.ID] = count
	}
	return out
}
