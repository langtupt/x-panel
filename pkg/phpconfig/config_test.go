package phpconfig

import "testing"

func TestSiteLimits(t *testing.T) {
	for _, c := range []Site{{Settings: map[string]string{"opcache.memory_consumption": "256"}}, {Workers: 51}, {Workers: -1}, {RequestTimeout: 3601}, {RequestTimeout: 10, SlowTimeout: 10}, {Settings: map[string]string{"open_basedir": "/"}}, {Settings: map[string]string{"memory_limit": "128M\nextension=evil"}}} {
		if c.Validate() == nil {
			t.Fatalf("unsafe config accepted: %+v", c)
		}
	}
	if err := (Site{Settings: map[string]string{"memory_limit": "256M", "display_errors": "0"}, Workers: 2, RequestTimeout: 60, SlowTimeout: 5}).Validate(); err != nil {
		t.Fatal(err)
	}
}
