package tenancy

import "testing"

// TestRoleOrderingAndValidity locks the RBAC ordering invariants that
// RequireRole relies on (F-TY-005) and the platform-role rules (X-TN-001).
func TestRoleOrderingAndValidity(t *testing.T) {
	// Strictly increasing privilege: viewer < editor < admin < platform.
	if !(RoleViewer.rank() < RoleEditor.rank() &&
		RoleEditor.rank() < RoleAdmin.rank() &&
		RoleAdmin.rank() < RolePlatform.rank()) {
		t.Fatalf("ranks not strictly increasing: viewer=%d editor=%d admin=%d platform=%d",
			RoleViewer.rank(), RoleEditor.rank(), RoleAdmin.rank(), RolePlatform.rank())
	}

	atLeast := []struct {
		r, req Role
		want   bool
	}{
		{RolePlatform, RoleAdmin, true},  // platform outranks admin
		{RoleAdmin, RolePlatform, false}, // admin does NOT reach platform
		{RoleAdmin, RoleAdmin, true},
		{RoleViewer, RoleAdmin, false},
		{RoleAdmin, RoleViewer, true},
		{"", RoleViewer, false},            // unknown caller never qualifies
		{RoleAdmin, Role("bogus"), false},  // unknown required rejects everyone
		{RolePlatform, Role(""), false},    // empty required rejects everyone
	}
	for _, c := range atLeast {
		if got := c.r.AtLeast(c.req); got != c.want {
			t.Errorf("%q.AtLeast(%q) = %v, want %v", c.r, c.req, got, c.want)
		}
	}

	// AllRoles is the canonical set: every entry must be valid and the set
	// must be exactly the four known roles in ascending order.
	want := []Role{RoleViewer, RoleEditor, RoleAdmin, RolePlatform}
	got := AllRoles()
	if len(got) != len(want) {
		t.Fatalf("AllRoles() = %v, want %v", got, want)
	}
	for i, r := range got {
		if r != want[i] {
			t.Errorf("AllRoles()[%d] = %q, want %q", i, r, want[i])
		}
		if !r.IsValid() {
			t.Errorf("%q from AllRoles() should be valid", r)
		}
	}
	if Role("bogus").IsValid() {
		t.Error("bogus role should be invalid")
	}

	// IsAssignableViaAPI = every valid role EXCEPT platform (bootstrap-only).
	for _, r := range []Role{RoleViewer, RoleEditor, RoleAdmin} {
		if !r.IsAssignableViaAPI() {
			t.Errorf("%q should be API-assignable", r)
		}
	}
	if RolePlatform.IsAssignableViaAPI() {
		t.Error("platform role must NOT be assignable via the API (X-TN-001)")
	}
	if Role("bogus").IsAssignableViaAPI() {
		t.Error("bogus role must not be API-assignable")
	}
}
