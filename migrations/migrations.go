package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

//go:embed */*.up.sql
var embedded embed.FS

// Embedded is the migration tree compiled into this binary.
func Embedded() fs.FS { return embedded }

// LatestVersions reports the highest migration version present for each module
// directory in fsys.
//
// Versions are compared numerically, not lexically: golang-migrate zero-pads to
// six digits today, but the padding is a convention rather than a guarantee, and
// a string comparison would order 000010 before 000002.
func LatestVersions(fsys fs.FS) (map[string]int, error) {
	latest := make(map[string]int)

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations root: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		module := entry.Name()
		files, err := fs.ReadDir(fsys, module)
		if err != nil {
			return nil, fmt.Errorf("read migrations for %q: %w", module, err)
		}
		for _, file := range files {
			name := file.Name()
			if !strings.HasSuffix(name, ".up.sql") {
				continue
			}
			version, err := parseVersion(name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path.Join(module, name), err)
			}
			if version > latest[module] {
				latest[module] = version
			}
		}
	}

	return latest, nil
}

func parseVersion(filename string) (int, error) {
	prefix, _, found := strings.Cut(filename, "_")
	if !found {
		return 0, fmt.Errorf("migration filename has no version prefix")
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration version %q is not a number: %w", prefix, err)
	}
	return version, nil
}
