package storage

import (
	"fmt"
	"strings"
	"testing"
)

// queryPlan runs EXPLAIN QUERY PLAN and flattens every row into one string,
// robust to the differing column counts modernc/sqlite returns.
func queryPlan(t *testing.T, store *SQLiteStore, query string, args ...any) string {
	t.Helper()
	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var plan strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		for _, v := range vals {
			fmt.Fprintf(&plan, "%v ", v)
		}
		plan.WriteByte('\n')
	}
	return plan.String()
}

// TestCompositeTenantIndex_ServesDashboardQuery is the PERF-13 guard: the
// tenant-scoped dashboard query must be served by the composite
// (tenant_id, id DESC) index for BOTH the filter and the ordering, so SQLite
// neither scans the whole table nor builds a temp B-tree to sort.
func TestCompositeTenantIndex_ServesDashboardQuery(t *testing.T) {
	store := testStore(t)

	plan := queryPlan(t, store,
		`SELECT id, timestamp FROM interaction_logs WHERE tenant_id = ? ORDER BY id DESC LIMIT ?`,
		"ten_acme", 50)

	if !strings.Contains(plan, "idx_logs_tenant_id") {
		t.Fatalf("dashboard query does not use the composite index idx_logs_tenant_id:\n%s", plan)
	}
	if strings.Contains(strings.ToUpper(plan), "TEMP B-TREE") {
		t.Fatalf("dashboard query still builds a temp B-tree for ORDER BY (index not serving the sort):\n%s", plan)
	}
}

// TestRedundantTenantIndex_Dropped verifies migrate() removed the now-redundant
// single-column idx_logs_tenant (the composite's leftmost column covers it).
func TestRedundantTenantIndex_Dropped(t *testing.T) {
	store := testStore(t)
	var n int
	if err := store.db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_logs_tenant'`,
	).Scan(&n); err != nil {
		t.Fatalf("querying sqlite_master: %v", err)
	}
	if n != 0 {
		t.Error("redundant single-column idx_logs_tenant was not dropped by migrate()")
	}
}
