package users

import "testing"

func ptr(v int64) *int64 { return &v }

func TestCanCreate(t *testing.T) {
	admin := &User{ID: 1, Role: RoleAdmin}
	reseller := &User{ID: 2, Role: RoleReseller, ParentID: ptr(1)}
	user := &User{ID: 3, Role: RoleUser, ParentID: ptr(2)}

	cases := []struct {
		actor *User
		role  Role
		want  bool
	}{
		{admin, RoleReseller, true},
		{admin, RoleUser, true},
		{admin, RoleAdmin, false},
		{reseller, RoleUser, true},
		{reseller, RoleReseller, false},
		{user, RoleUser, false},
	}
	for _, c := range cases {
		if got := CanCreate(c.actor, c.role); got != c.want {
			t.Errorf("CanCreate(%s, %s) = %v, want %v", c.actor.Role, c.role, got, c.want)
		}
	}
}

func TestCanManage(t *testing.T) {
	admin := &User{ID: 1, Role: RoleAdmin}
	admin2 := &User{ID: 9, Role: RoleAdmin}
	resA := &User{ID: 2, Role: RoleReseller, ParentID: ptr(1)}
	resB := &User{ID: 4, Role: RoleReseller, ParentID: ptr(1)}
	userA := &User{ID: 3, Role: RoleUser, ParentID: ptr(2)}

	cases := []struct {
		name          string
		actor, target *User
		want          bool
	}{
		{"admin manages reseller", admin, resA, true},
		{"admin manages any user", admin, userA, true},
		{"admin cannot manage admin", admin, admin2, false},
		{"admin cannot manage self", admin, admin, false},
		{"reseller manages own user", resA, userA, true},
		{"reseller cannot manage other reseller's user", resB, userA, false},
		{"reseller cannot manage reseller", resA, resB, false},
		{"user manages nobody", userA, resA, false},
	}
	for _, c := range cases {
		if got := CanManage(c.actor, c.target); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateNew(t *testing.T) {
	if err := ValidateNew("client01", "longenough1", "a@b.vn"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Root", "1abc", "ab", "root", "xpanel", "has-dash"} {
		if ValidateNew(bad, "longenough1", "") == nil {
			t.Errorf("username %q should be rejected", bad)
		}
	}
	if ValidateNew("client01", "short", "") == nil {
		t.Error("short password should be rejected")
	}
}
