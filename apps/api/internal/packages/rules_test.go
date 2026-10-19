package packages

import (
	"testing"

	"xpanel/apps/api/internal/users"
)

func TestValidate(t *testing.T) {
	p := &Package{Name: " Basic ", Kind: users.RoleUser, Oversell: true,
		Limits: Limits{DiskMB: 1024, MaxUsers: 5, PHPVersions: []string{"8.3", "8.1", "8.3"}}}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Basic" || p.MaxUsers != 0 || p.Oversell || len(p.PHPVersions) != 2 || p.PHPVersions[0] != "8.1" {
		t.Fatalf("not normalised: %+v", p)
	}
	bad := []*Package{
		{Name: "", Kind: users.RoleUser},
		{Name: "x", Kind: users.RoleAdmin},
		{Name: "x", Kind: users.RoleUser, Limits: Limits{DiskMB: -1}},
		{Name: "x", Kind: users.RoleUser, Limits: Limits{PHPVersions: []string{"5.6"}}},
	}
	for i, b := range bad {
		if Validate(b) == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestWithinParent(t *testing.T) {
	parent := Limits{DiskMB: 10240, MaxSites: 10, MaxUsers: 3, PHPVersions: []string{"8.2", "8.3"}}
	ok := Limits{DiskMB: 2048, MaxSites: 5, PHPVersions: []string{"8.3"}}
	if err := WithinParent(ok, parent); err != nil {
		t.Fatal(err)
	}
	cases := map[string]Limits{
		"disk too big":    {DiskMB: 20480, MaxSites: 1, PHPVersions: []string{"8.3"}},
		"unlimited disk":  {DiskMB: 0, MaxSites: 1, PHPVersions: []string{"8.3"}},
		"unlimited sites": {DiskMB: 1, MaxSites: 0, PHPVersions: []string{"8.3"}},
		"php not allowed": {DiskMB: 1, MaxSites: 1, PHPVersions: []string{"7.4"}},
		"php missing":     {DiskMB: 1, MaxSites: 1},
	}
	for name, c := range cases {
		if WithinParent(c, parent) == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// Unlimited parent fields impose nothing.
	if err := WithinParent(Limits{}, Limits{}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCapacity(t *testing.T) {
	res := Limits{DiskMB: 10000, MaxUsers: 3}
	if err := CheckCapacity(res, false, 2, 6000, 1, 4000); err != nil {
		t.Fatalf("exactly full should pass: %v", err)
	}
	if CheckCapacity(res, false, 3, 0, 1, 0) == nil {
		t.Error("4th user should fail")
	}
	if CheckCapacity(res, false, 1, 9000, 1, 2000) == nil {
		t.Error("disk over pool should fail")
	}
	if err := CheckCapacity(res, true, 1, 9000, 1, 2000); err != nil {
		t.Errorf("oversell should allow disk over pool: %v", err)
	}
	if CheckCapacity(res, true, 3, 0, 1, 0) == nil {
		t.Error("oversell must still enforce account limit")
	}
	if err := CheckCapacity(Limits{}, false, 100, 1e6, 1, 1e6); err != nil {
		t.Errorf("unlimited reseller: %v", err)
	}
}
