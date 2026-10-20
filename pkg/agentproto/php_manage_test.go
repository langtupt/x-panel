package agentproto

import "testing"

func TestManageRequestValidation(t *testing.T) {
	for _, p := range []PHPManageParams{{Version: "8.5", Operation: "refresh", Remove: true}, {Version: "8.5", Operation: "disable"}, {Version: "8.5", Module: "php8.5-curl", Remove: true, Operation: "enable"}, {Version: "8.5", Module: "php8.5-curl", Settings: map[string]string{"memory_limit": "64M"}}} {
		if p.Validate() == nil {
			t.Fatalf("invalid operation accepted: %+v", p)
		}
	}
	for _, p := range []PHPManageParams{{Version: "8.5", Operation: "refresh"}, {Version: "8.5", Module: "php8.5-curl", Operation: "update"}, {Version: "8.5", Reset: []string{"memory_limit"}}} {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
