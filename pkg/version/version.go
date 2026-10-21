// Package version holds the X-Panel release, set when building a release:
//
//	go build -ldflags "-X xpanel/pkg/version.Version=v1.2.6"
package version

import (
	"regexp"
	"strconv"
)

// Version is "dev" for builds made outside scripts/build-release.sh.
var Version = "dev"

var releaseRE = regexp.MustCompile(`^v([0-9]{1,4})\.([0-9]{1,4})\.([0-9]{1,4})$`)

// Valid reports whether v is a release tag such as v1.2.6.
func Valid(v string) bool { return releaseRE.MatchString(v) }

// Newer reports whether release a is newer than release b. Anything that is
// not a release tag (a dev build) is never newer, and every release is newer
// than a dev build.
func Newer(a, b string) bool {
	ma := releaseRE.FindStringSubmatch(a)
	if ma == nil {
		return false
	}
	mb := releaseRE.FindStringSubmatch(b)
	if mb == nil {
		return true
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
