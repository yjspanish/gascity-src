package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGCBeadsBDScript_DoesNotConfigSetIssuePrefix guards against a regression
// where gc-beads-bd.sh op_init seeded issue_prefix via `bd config set
// issue_prefix`. bd 1.0.3+ explicitly rejects that command (issue_prefix is
// immutable post-init; only `bd init --prefix` or `bd rename-prefix` may set
// it). When the call silently fails (gc-beads-bd.sh wraps it in `2>/dev/null
// || true`), the dolt config table is left without the row, and every
// subsequent `bd create` against that scope dies with "issue_prefix config is
// missing". Including supervisor-internal order dispatch (mol-dog-reaper,
// orphan-sweep), so the whole reconciler is broken until the row is seeded.
func TestGCBeadsBDScript_DoesNotConfigSetIssuePrefix(t *testing.T) {
	script := readGCBeadsBDScript(t)
	// Look for the actual invocation pattern (`config set issue_prefix "$prefix"`),
	// not prose mentions in comments. The historical bug was a `run_bd_pinned ...
	// config set issue_prefix "$prefix"` call wrapped in `2>/dev/null || true`.
	for _, banned := range []string{
		`config set issue_prefix "$prefix"`,
		`config set issue_prefix "${prefix}"`,
		`config set issue_prefix $prefix`,
	} {
		if strings.Contains(script, banned) {
			t.Fatalf("gc-beads-bd.sh must not seed issue_prefix via 'bd config set issue_prefix' "+
				"(rejected by bd >=1.0.3); found %q. Use SQL insert into the scope's dolt config table instead.", banned)
		}
	}
}

// TestGCBeadsBDScript_SeedsIssuePrefixDirectly asserts that gc-beads-bd.sh
// writes the issue_prefix row to the scope's dolt config table itself. bd init
// in --server mode may run against a pre-created database (created by
// ensure_database_registered) and not write the row; gc owns the fix.
func TestGCBeadsBDScript_SeedsIssuePrefixDirectly(t *testing.T) {
	script := readGCBeadsBDScript(t)
	// Match the SQL invocation bd uses to keep config rows: the row lives in
	// the scope's `config` table keyed by `issue_prefix`. Either the literal
	// table+key or a helper that wraps the upsert is fine; assert at least one
	// of the two recognizable forms is present.
	hasUpsert := strings.Contains(script, "INSERT INTO config") ||
		strings.Contains(script, "REPLACE INTO config")
	mentionsKey := strings.Contains(script, "issue_prefix")
	if !(hasUpsert && mentionsKey) {
		t.Fatalf("gc-beads-bd.sh must seed issue_prefix into the scope dolt config table " +
			"via an INSERT/REPLACE INTO config statement keyed by 'issue_prefix'")
	}
}

// TestGCBeadsBDScript_SeedIssuePrefixOrderedAfterConfigSet captures the
// ordering invariant uncovered by gascity-c3u. bd init --server -p <prefix>
// installs the schema in the orphan beads_<prefix> database, which op_init
// then drops. The pinned dolt database (created earlier by
// ensure_database_registered) is left empty — no config table. A direct
// INSERT INTO config therefore fails with "table not found: config".
//
// The first `bd config set ...` call lazily migrates the schema into the
// pinned database, creating the config table as a side effect. Every
// seed_issue_prefix call must therefore be preceded (within its op_init
// branch) by a `bd config set` invocation against the same scope.
//
// Without this ordering, gc rig add against a fresh prefix succeeds in name
// only — bd create then dies with "issue_prefix config is missing".
func TestGCBeadsBDScript_SeedIssuePrefixOrderedAfterConfigSet(t *testing.T) {
	script := readGCBeadsBDScript(t)
	const seedCall = `seed_issue_prefix "$dolt_database" "$prefix"`
	const primeCall = `config set types.custom`

	searchFrom := 0
	pairs := 0
	for {
		seedAt := strings.Index(script[searchFrom:], seedCall)
		if seedAt < 0 {
			break
		}
		seedAt += searchFrom
		// The priming call must appear before this seed call AND no later than
		// the prior seed_issue_prefix call (i.e., inside the same op_init
		// branch). We approximate "same branch" by requiring the priming call
		// to fall within the slice [searchFrom, seedAt).
		primeAt := strings.LastIndex(script[searchFrom:seedAt], primeCall)
		if primeAt < 0 {
			t.Fatalf("seed_issue_prefix at offset %d has no preceding %q in its op_init branch — "+
				"the dolt config table is created lazily by `bd config set`, so seeding via SQL "+
				"first hits 'table not found: config' (gascity-c3u).", seedAt, primeCall)
		}
		searchFrom = seedAt + len(seedCall)
		pairs++
	}
	if pairs == 0 {
		t.Fatalf("no %q calls found — script must seed issue_prefix into the dolt config table", seedCall)
	}
}

func readGCBeadsBDScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	scriptPath := filepath.Join(filepath.Dir(thisFile), "..", "..",
		"examples", "bd", "assets", "scripts", "gc-beads-bd.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	return string(data)
}
