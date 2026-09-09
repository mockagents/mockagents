package audit

import "testing"

// TestSchemaHasTenantIndex is the audit M-14 guard: the multi-tenant read
// path always filters on actor_tenant and orders by id DESC, so that pair
// must be indexed or every dashboard load scans the whole table.
func TestSchemaHasTenantIndex(t *testing.T) {
	s := newTestStore(t)
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_audit_tenant_id'`).Scan(&name)
	if err != nil {
		t.Fatalf("idx_audit_tenant_id missing: %v", err)
	}
	// And the planner uses it for the tenant-scoped listing.
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM audit_events WHERE actor_tenant = ? ORDER BY id DESC LIMIT 10`, "ten_x")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if !contains(plan, "idx_audit_tenant_id") {
		t.Fatalf("query plan does not use idx_audit_tenant_id:\n%s", plan)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
