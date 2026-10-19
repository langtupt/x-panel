package apps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"regexp"
	"xpanel/apps/agent/internal/fetch"
	"xpanel/apps/agent/internal/run"
)

const (
	NodeRoot     = "/opt/xpanel/node"
	GoRoot       = "/opt/xpanel/go"
	ComposerPath = "/usr/local/bin/composer"
)

// NodeDir is the install directory of a Node.js major line ("22").
func NodeDir(major string) string { return filepath.Join(NodeRoot, major) }

// GoDir is the GOROOT of an installed Go version ("1.27.2").
func GoDir(version string) string { return filepath.Join(GoRoot, version) }

func installedDirs(root, probe string) []string {
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasSuffix(e.Name(), ".new") && !strings.HasSuffix(e.Name(), ".old") {
			if _, err := os.Stat(filepath.Join(root, e.Name(), probe)); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sortVersionsDesc(out)
	return out
}

func NodeInstalled() []string { return installedDirs(NodeRoot, "bin/node") }
func GoInstalled() []string   { return installedDirs(GoRoot, "bin/go") }

type nodeRelease struct {
	Version string   `json:"version"` // "v22.11.0"
	LTS     any      `json:"lts"`     // false or codename
	Files   []string `json:"files"`
}

func nodeReleases(ctx context.Context) ([]nodeRelease, error) {
	var rel []nodeRelease
	err := fetch.JSON(ctx, "https://nodejs.org/dist/index.json", &rel)
	return rel, err
}

func nodeArch() string {
	if goArch() == "arm64" {
		return "arm64"
	}
	return "x64"
}

// latestNode returns the newest release of each major line, keyed by major.
func latestNode(rel []nodeRelease) map[string]nodeRelease {
	out := map[string]nodeRelease{}
	for _, r := range rel { // index.json is newest first
		major := strings.SplitN(strings.TrimPrefix(r.Version, "v"), ".", 2)[0]
		if _, seen := out[major]; !seen {
			out[major] = r
		}
	}
	return out
}

// nodeAvailable offers the current line plus the three newest LTS lines.
func nodeAvailable(ctx context.Context) ([]string, error) {
	rel, err := nodeReleases(ctx)
	if err != nil {
		return nil, err
	}
	return pickNodeLines(rel), nil
}

// pickNodeLines keeps the newest major (Current) and the three newest LTS majors.
func pickNodeLines(rel []nodeRelease) []string {
	latest := latestNode(rel)
	var majors []string
	for m := range latest {
		majors = append(majors, m)
	}
	sortVersionsDesc(majors)
	var out []string
	lts := 0
	for i, m := range majors {
		r := latest[m]
		isLTS := r.LTS != false && r.LTS != nil
		if i == 0 || (isLTS && lts < 3) {
			out = append(out, m)
			if isLTS {
				lts++
			}
		}
	}
	return out
}

func installNode(ctx context.Context, major string, logf func(string, ...any)) error {
	if _, err := strconv.Atoi(major); err != nil {
		return fmt.Errorf("invalid Node.js line %q", major)
	}
	rel, err := nodeReleases(ctx)
	if err != nil {
		return err
	}
	r, ok := latestNode(rel)[major]
	if !ok {
		return fmt.Errorf("không tìm thấy Node.js %s", major)
	}
	name := fmt.Sprintf("node-%s-linux-%s.tar.gz", r.Version, nodeArch())
	base := "https://nodejs.org/dist/" + r.Version + "/"
	sums, err := fetch.Text(ctx, base+"SHASUMS256.txt")
	if err != nil {
		return err
	}
	sum := ""
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == name {
			sum = f[0]
		}
	}
	if sum == "" {
		return fmt.Errorf("không có checksum cho %s", name)
	}
	logf("Tải %s%s", base, name)
	archive, err := fetch.File(ctx, base+name, sum)
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	logf("Đã kiểm tra SHA-256, giải nén vào %s", NodeDir(major))
	if err := fetch.Replace(NodeDir(major), func(tmp string) error { return fetch.UntarGz(archive, tmp, 1) }); err != nil {
		return err
	}
	logf("Đã cài Node.js %s", strings.TrimPrefix(r.Version, "v"))
	return nil
}

type goRelease struct {
	Version string `json:"version"` // "go1.27.2"
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	} `json:"files"`
}

func goReleases(ctx context.Context) ([]goRelease, error) {
	var rel []goRelease
	err := fetch.JSON(ctx, "https://go.dev/dl/?mode=json", &rel)
	return rel, err
}

func goAvailable(ctx context.Context) ([]string, error) {
	rel, err := goReleases(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rel {
		if r.Stable {
			out = append(out, strings.TrimPrefix(r.Version, "go"))
		}
	}
	sortVersionsDesc(out)
	return out, nil
}

func installGo(ctx context.Context, version string, logf func(string, ...any)) error {
	rel, err := goReleases(ctx)
	if err != nil {
		return err
	}
	for _, r := range rel {
		if r.Version != "go"+version {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == "linux" && f.Arch == goArch() {
				logf("Tải https://go.dev/dl/%s", f.Filename)
				archive, err := fetch.File(ctx, "https://go.dev/dl/"+f.Filename, f.SHA256)
				if err != nil {
					return err
				}
				defer os.Remove(archive)
				logf("Đã kiểm tra SHA-256, giải nén vào %s", GoDir(version))
				if err := fetch.Replace(GoDir(version), func(tmp string) error { return fetch.UntarGz(archive, tmp, 1) }); err != nil {
					return err
				}
				logf("Đã cài Go %s", version)
				return nil
			}
		}
	}
	return fmt.Errorf("không tìm thấy bản Go %s cho linux/%s", version, goArch())
}

func composerInstalled() []string {
	out, err := run.Output(context.Background(), ComposerPath, "--version", "--no-ansi")
	if err == nil {
		if m := regexp.MustCompile(`Composer version ([0-9.]+)`).FindStringSubmatch(out); len(m) > 1 {
			return []string{m[1]}
		}
	}
	return nil
}

func installComposer(ctx context.Context, version string, logf func(string, ...any)) error {
	if !numericVersion.MatchString(version) {
		return fmt.Errorf("invalid Composer version")
	}
	base := "https://getcomposer.org/download/" + version + "/composer.phar"
	sumLine, err := fetch.Text(ctx, base+".sha256sum")
	if err != nil {
		return err
	}
	f := strings.Fields(sumLine)
	if len(f) == 0 {
		return fmt.Errorf("empty composer checksum")
	}
	logf("Tải %s", base)
	tmp, err := fetch.File(ctx, base, f[0])
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	if err := os.WriteFile(ComposerPath+".new", b, 0o755); err != nil {
		return err
	}
	if err := os.Rename(ComposerPath+".new", ComposerPath); err != nil {
		return err
	}
	logf("Đã cài Composer vào %s (đã kiểm tra SHA-256)", ComposerPath)
	return nil
}
