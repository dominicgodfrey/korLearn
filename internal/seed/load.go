package seed

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// LoadDir parses every .json file under fsys, recursively, and returns the
// lessons ordered by position.
//
// It fails on the first structural problem it cannot attribute to a single file
// (a duplicate position, say) but collects per-file parse errors so that one
// broken lesson does not hide the other nine.
func LoadDir(fsys fs.FS) ([]Lesson, error) {
	var (
		lessons []Lesson
		names   []string // parallel to lessons, for error messages
		errs    []error
	)

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(path.Ext(p), ".json") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		l, err := Parse(p, data)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		lessons = append(lessons, l)
		names = append(names, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	// Position is the single global ordering the whole app depends on: a
	// collision would make "unlocked" ambiguous and silently reorder study.
	byPosition := make(map[int]string, len(lessons))
	for i, l := range lessons {
		if prev, dup := byPosition[l.Position]; dup {
			errs = append(errs, fmt.Errorf("position %d used by both %s and %s", l.Position, prev, names[i]))
			continue
		}
		byPosition[l.Position] = names[i]
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.SortFunc(lessons, func(a, b Lesson) int { return a.Position - b.Position })
	return lessons, nil
}
