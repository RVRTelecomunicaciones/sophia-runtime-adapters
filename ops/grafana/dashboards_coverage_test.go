//go:build dashboards

// Package grafana_test enforces the dashboard contract from spec §10:
//
//   - Dashboard UIDs are pinned to a known canonical set and all unique.
//   - Per-adapter dashboards declare the `capability` templating variable
//     (§10.5) — the deep-link deliveries from runtime-overview rely on
//     that name.
//   - Every PromQL metric / recording-rule identifier referenced by a
//     dashboard target resolves to either (a) a name declared in a
//     Sloth-generated or hand-written Prometheus rules file, or (b) the
//     Bundle 2 runtime-emitted instrument catalog (§6.3), or (c) the
//     stable allowlist of external-system metrics we knowingly query
//     (pgx pool histograms etc).
//
// Drift between ops/grafana/ and the metric / rule contract breaks CI,
// mirroring the spirit of Phase 1's VerifyCoversPhase1Catalog.
//
// Build-tagged `dashboards` so dev machines can skip without break — CI
// always runs with -tags dashboards per Bundle 8.
//
// Spec §10.2, §10.3, §11.2.
package grafana_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// canonicalUIDs is the pinned UID set (spec §10.2). Any UID change here
// requires an ADR (breaks dashboard links + embed URLs).
var canonicalUIDs = map[string]bool{
	"runtime-overview": true,
	"runtime-shell":    true,
	"runtime-git":      true,
	"runtime-fs":       true,
	"runtime-http":     true,
}

// perAdapterUIDs are the dashboards that MUST carry a `capability`
// templating variable. The overview deliberately omits it.
var perAdapterUIDs = map[string]bool{
	"runtime-shell": true,
	"runtime-git":   true,
	"runtime-fs":    true,
	"runtime-http":  true,
}

// runtimeEmittedInstruments mirrors the Bundle 2 instrument catalog
// (internal/infrastructure/obs/metrics.go InstrumentCatalog). Each entry
// is the OTel instrument name with dots swapped for underscores — which
// is how it surfaces in Prometheus after the OTel → Prometheus
// exporter's naming policy runs. Histograms appear additionally as
// <name>_bucket, <name>_count, <name>_sum; we derive those here so a
// dashboard referencing `foo_seconds_bucket` resolves correctly.
//
// Mirroring (not importing) keeps this ops/ test free of cross-tree
// Go dependencies — same pattern as ops/slo cannot import adapter
// registries. Drift is caught by metrics_test.go which owns the
// authoritative InstrumentCatalog contract.
var runtimeEmittedInstruments = []instrument{
	{name: "runtime_adapters_execution_total", kind: counter},
	{name: "runtime_adapters_execution_duration_seconds", kind: histogram},
	{name: "runtime_adapters_execution_active", kind: gauge},
	{name: "runtime_adapters_concurrency_rejects", kind: counter},
	{name: "runtime_adapters_adapter_panics", kind: counter},
	{name: "runtime_adapters_receipt_persist_failures", kind: counter},
	{name: "runtime_adapters_idempotency_replays", kind: counter},
	{name: "runtime_adapters_pool_connections_acquired_duration_seconds", kind: histogram},
	{name: "runtime_adapters_otel_exporter_queue_size", kind: gauge},
	{name: "runtime_adapters_migrate_failures", kind: counter},
	{name: "runtime_adapters_partial_signal", kind: counter},
}

// externalAllowlist names metrics the dashboards knowingly query from
// collectors outside the runtime process itself (e.g. pgx pool stats
// once the 2C.4 collector ships). Adding an entry is an explicit
// decision — no wildcards. Currently empty: dashboards only use the
// runtime-emitted + sloth-generated surface.
var externalAllowlist = map[string]bool{}

type kind int

const (
	counter kind = iota
	gauge
	histogram
)

type instrument struct {
	name string
	kind kind
}

// TestDashboards_UIDsCanonicalAndUnique verifies the pinned UID set
// (§10.2 — UIDs are a stability contract). Duplicates would cause
// Grafana to silently overwrite; unknown UIDs would mean a drift in
// dashboard inventory that our deep-links from overview cannot follow.
func TestDashboards_UIDsCanonicalAndUnique(t *testing.T) {
	dashes := loadDashboards(t)
	require.Len(t, dashes, len(canonicalUIDs),
		"expected %d dashboards under ops/grafana/dashboards/, got %d",
		len(canonicalUIDs), len(dashes))

	seen := map[string]string{}
	for _, d := range dashes {
		require.NotEmpty(t, d.UID, "dashboard %s missing uid", d.path)
		require.Truef(t, canonicalUIDs[d.UID],
			"dashboard %s declares unknown uid %q — not in canonical set", d.path, d.UID)
		if prior, dup := seen[d.UID]; dup {
			t.Fatalf("duplicate uid %q — seen in %s and %s", d.UID, prior, d.path)
		}
		seen[d.UID] = d.path
	}

	// Every canonical UID must be present exactly once.
	for uid := range canonicalUIDs {
		require.Contains(t, seen, uid,
			"canonical uid %q not found in ops/grafana/dashboards/", uid)
	}
}

// TestDashboards_PerAdapterHasCapabilityVariable asserts that every
// per-adapter dashboard declares a templating variable named
// `capability` (§10.5). The overview's data links carry
// `var-capability=<capability>` — renaming or removing the variable
// breaks that deep-link without any runtime error.
func TestDashboards_PerAdapterHasCapabilityVariable(t *testing.T) {
	dashes := loadDashboards(t)
	for _, d := range dashes {
		if !perAdapterUIDs[d.UID] {
			continue
		}
		found := false
		for _, v := range d.Templating.List {
			if v.Name == "capability" {
				found = true
				break
			}
		}
		require.Truef(t, found,
			"per-adapter dashboard %s (uid=%s) missing templating variable 'capability'",
			d.path, d.UID)
	}
}

// TestDashboards_QueryReferencesResolve walks every panel target in
// every dashboard, extracts every PromQL metric / recording-rule
// identifier, and asserts each resolves to either an upstream rule
// file, the runtime instrument catalog, or the declared external
// allowlist. Identifiers that match nothing fail the test with the
// specific dashboard/panel/refId context so drift is caught early
// (§10.3 — dashboards bind to real generator output, not assumed names).
func TestDashboards_QueryReferencesResolve(t *testing.T) {
	repoRoot := findRepoRoot(t)

	recordedRules := collectRecordedRuleNames(t, repoRoot)
	emittedNames := expandInstruments(runtimeEmittedInstruments)

	resolved := map[string]bool{}
	for n := range recordedRules {
		resolved[n] = true
	}
	for n := range emittedNames {
		resolved[n] = true
	}
	for n := range externalAllowlist {
		resolved[n] = true
	}

	dashes := loadDashboards(t)
	require.NotEmpty(t, dashes, "no dashboards loaded")

	for _, d := range dashes {
		for _, p := range walkPanels(d.Panels) {
			for _, tgt := range p.Targets {
				expr := strings.TrimSpace(tgt.Expr)
				if expr == "" {
					continue
				}
				for _, ref := range extractMetricNames(expr) {
					if !resolved[ref] {
						t.Errorf(
							"dashboard %s panel %q target %s references unknown metric/rule %q\n  expr: %s",
							d.path, p.Title, tgt.RefID, ref, expr)
					}
				}
			}
		}
	}
}

// --- helpers ---

// dashboard is the minimal subset of the Grafana schema this test
// reads. JSON tags match the on-disk dashboard format.
type dashboard struct {
	UID        string    `json:"uid"`
	Title      string    `json:"title"`
	Templating templates `json:"templating"`
	Panels     []panel   `json:"panels"`
	path       string    `json:"-"`
}

type templates struct {
	List []templateVar `json:"list"`
}

type templateVar struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type panel struct {
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Panels  []panel  `json:"panels"` // nested for row panels
	Targets []target `json:"targets"`
}

type target struct {
	RefID string `json:"refId"`
	Expr  string `json:"expr"`
}

func loadDashboards(t *testing.T) []dashboard {
	t.Helper()
	repoRoot := findRepoRoot(t)
	files, err := filepath.Glob(filepath.Join(repoRoot, "ops", "grafana", "dashboards", "*.json"))
	require.NoError(t, err)
	sort.Strings(files)

	var out []dashboard
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err, "read %s", f)
		var d dashboard
		require.NoError(t, json.Unmarshal(data, &d), "parse %s", f)
		d.path = f
		out = append(out, d)
	}
	return out
}

// walkPanels flattens panels and any nested row-style panel children
// into a single slice. Grafana's row panels carry their children under
// `panels:` (same JSON field). Dashboards in this repo do not currently
// nest, but the walker is future-proofed.
func walkPanels(ps []panel) []panel {
	var out []panel
	for _, p := range ps {
		out = append(out, p)
		if len(p.Panels) > 0 {
			out = append(out, walkPanels(p.Panels)...)
		}
	}
	return out
}

// metricNameRE matches identifiers that LOOK like metric / recording-
// rule names in PromQL: either colon-separated (recording rules —
// `slo:sli_error:ratio_rate5m`) or bare Prometheus-style metrics
// (`runtime_adapters_execution_total`, `sloth_slo_info`,
// `pgx_pool_acquire_count`, etc). We intentionally skip PromQL keywords
// (`rate`, `sum`, etc) via a denylist because they match the same regex
// shape.
//
// Strategy: pull every candidate token, then filter.
var metricNameRE = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*(?::[a-zA-Z_][a-zA-Z0-9_]*)*`)

// promqlKeywords is the denylist of PromQL builtin tokens that share
// the identifier shape. Sourced from the Prometheus PromQL grammar —
// operators, aggregation functions, vector-matching keywords, and
// common instant/range functions used by our dashboards.
var promqlKeywords = map[string]bool{
	// Aggregation operators.
	"sum": true, "min": true, "max": true, "avg": true, "group": true,
	"stddev": true, "stdvar": true, "count": true, "count_values": true,
	"bottomk": true, "topk": true, "quantile": true,
	// Aggregation modifiers.
	"by": true, "without": true, "on": true, "ignoring": true,
	"group_left": true, "group_right": true,
	// Binary comparison operators.
	"and": true, "or": true, "unless": true,
	// Range / instant functions commonly seen in dashboards.
	"rate": true, "irate": true, "increase": true, "delta": true,
	"idelta": true, "deriv": true, "predict_linear": true,
	"histogram_quantile": true, "histogram_count": true,
	"histogram_sum": true, "histogram_fraction": true,
	"abs": true, "absent": true, "absent_over_time": true,
	"ceil": true, "clamp": true, "clamp_max": true, "clamp_min": true,
	"day_of_month": true, "day_of_week": true, "days_in_month": true,
	"exp": true, "floor": true, "hour": true, "ln": true, "log10": true,
	"log2": true, "minute": true, "month": true, "year": true,
	"round": true, "scalar": true, "sgn": true, "sort": true,
	"sort_desc": true, "sqrt": true, "time": true, "timestamp": true,
	"vector": true, "label_replace": true, "label_join": true,
	"changes": true, "resets": true, "sort_by_label": true,
	"sort_by_label_desc": true,
	// over_time family.
	"avg_over_time": true, "min_over_time": true, "max_over_time": true,
	"sum_over_time": true, "count_over_time": true,
	"stddev_over_time": true, "stdvar_over_time": true,
	"quantile_over_time": true, "last_over_time": true,
	"present_over_time": true,
	// Label-filter keywords that show up inside matcher lists we skip
	// below, but belt-and-braces.
	"status": true, "capability": true, "adapter": true,
	"sloth_slo": true, "sloth_id": true, "sloth_service": true,
	"le": true,
	// Grafana template variables surface as bare words after
	// stripMatchers strips quotes — keep them out of the namespace.
	"__rate_interval": true, "__auto": true, "__all": true, "all": true,
	// Common label values we see in matchers (lowercased after the
	// matcher stripper).
	"success": true, "failure": true, "timeout": true, "cancelled": true,
	"partial": true, "warning": true, "critical": true, "info": true,
	"shell": true, "git": true, "filesystem": true, "http": true,
}

// extractMetricNames returns the metric / recording-rule identifiers
// referenced in a PromQL expression. It strips label-matcher sections
// (`{...}`) and range specifiers (`[...]`) first — those contain label
// keys and values that would otherwise look like identifiers.
func extractMetricNames(expr string) []string {
	// Strip label matchers: {foo="bar"}.
	expr = stripBalanced(expr, '{', '}')
	// Strip range specifiers: [5m], [$__rate_interval].
	expr = stripBalanced(expr, '[', ']')
	// Strip string literals inside `label_replace` style calls.
	expr = stripStrings(expr)

	seen := map[string]bool{}
	var out []string
	for _, match := range metricNameRE.FindAllString(expr, -1) {
		if promqlKeywords[match] {
			continue
		}
		// Skip pure numbers (PromQL doesn't allow leading digits in
		// identifiers so the regex won't emit them, but the guard is
		// cheap).
		if len(match) == 0 {
			continue
		}
		// Skip single-char tokens (`d`, `s`, `m`, `h` — range units
		// with no leading digit after stripping brackets aren't a
		// concern here because the whole bracket is gone, but future
		// edits might shape the expr differently).
		if len(match) < 2 {
			continue
		}
		if seen[match] {
			continue
		}
		seen[match] = true
		out = append(out, match)
	}
	return out
}

func stripBalanced(s string, open, close byte) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == open {
			depth++
			continue
		}
		if c == close && depth > 0 {
			depth--
			continue
		}
		if depth == 0 {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func stripStrings(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inStr = !inStr
			continue
		}
		if !inStr {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// collectRecordedRuleNames scans ops/prometheus/rules/*.yaml and
// ops/prometheus/generated/*.yaml for `- record:` entries and returns
// the set of rule names. Also pulls `- alert:` names because an alert
// rule that declares `labels:` also emits the alert series in
// Alertmanager-adjacent queries (not needed here yet, but collecting
// both keeps the allowlist lenient for future dashboards).
func collectRecordedRuleNames(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	names := map[string]bool{}

	patterns := []string{
		filepath.Join(repoRoot, "ops", "prometheus", "rules", "*.yaml"),
		filepath.Join(repoRoot, "ops", "prometheus", "generated", "*.yaml"),
	}
	var files []string
	for _, p := range patterns {
		matches, _ := filepath.Glob(p)
		files = append(files, matches...)
	}
	require.NotEmpty(t, files, "no prometheus rule files found under ops/prometheus/")

	recordRE := regexp.MustCompile(`^\s*-\s*record:\s*(\S+)`)
	alertRE := regexp.MustCompile(`^\s*-\s*alert:\s*(\S+)`)
	for _, f := range files {
		if strings.HasSuffix(f, ".test.yaml") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if m := recordRE.FindStringSubmatch(line); len(m) == 2 {
				names[m[1]] = true
			}
			if m := alertRE.FindStringSubmatch(line); len(m) == 2 {
				names[m[1]] = true
			}
		}
	}
	require.NotEmpty(t, names, "found no record/alert names in any rule file")
	return names
}

// expandInstruments maps each instrument to the Prometheus-side name
// shape the OTel exporter emits. Histograms surface as three series
// (<name>_bucket, <name>_count, <name>_sum); counters / gauges surface
// under the raw name. All forms are added to the resolved set.
func expandInstruments(is []instrument) map[string]bool {
	out := map[string]bool{}
	for _, i := range is {
		out[i.name] = true
		if i.kind == histogram {
			out[i.name+"_bucket"] = true
			out[i.name+"_count"] = true
			out[i.name+"_sum"] = true
		}
	}
	return out
}

// findRepoRoot walks up the filesystem until it finds a directory
// containing go.mod. Same pattern as ops/slo and ops/alertmanager.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find repo root (go.mod) starting from %s", cwd)
	return ""
}
