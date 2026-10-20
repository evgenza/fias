package importjob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"gar-importer/internal/gar"
)

var ErrReleaseBlocked = errors.New("предыдущий архив не завершён")

var ErrReleaseOrder = errors.New("недопустимый порядок выпусков")

// ValidateReleaseOrder checks the published chain rather than assuming daily releases.
func ValidateReleaseOrder(mode, applied, base, target string) error {
	if mode == "delta" {
		if applied == "" || base != applied || target <= base {
			return fmt.Errorf("%w: delta %s -> %s, applied=%s", ErrReleaseOrder, base, target, applied)
		}
	} else if applied != "" && target < applied {
		return fmt.Errorf("%w: snapshot=%s, applied=%s", ErrReleaseOrder, target, applied)
	}
	return nil
}

func releaseDate(value string) (string, error) {
	for _, layout := range []string{"20060102", "2006-01-02", "02.01.2006"} {
		if date, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return date.Format("20060102"), nil
		}
	}
	return "", fmt.Errorf("%w: неверная дата выпуска %q", ErrReleaseOrder, value)
}

func resolveRelease(source Archive, options *Options) error {
	if options.Mode == "" {
		options.Mode = ModeSnapshot
		if strings.Contains(strings.ToLower(sourcePath(source)), "gar_delta_xml") {
			options.Mode = "delta"
		}
	}
	if options.Mode != ModeSnapshot && options.Mode != "delta" {
		return fmt.Errorf("%w: режим %q", ErrReleaseOrder, options.Mode)
	}
	var fileRelease string
	for _, name := range source.Names() {
		if file, err := gar.ParseFile(path.Base(name)); err == nil && file.Date > fileRelease {
			fileRelease = file.Date
		}
		if strings.EqualFold(path.Base(name), "version.txt") && options.TargetRelease == "" {
			reader, err := source.Open(name)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(reader, 1025))
			closeErr := reader.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			options.TargetRelease = strings.TrimSpace(string(data))
		}
	}
	if options.TargetRelease == "" {
		options.TargetRelease = fileRelease
	}
	// Empty/unrecognized archives still report ErrNoSelectedFiles in the normal planner.
	if options.TargetRelease == "" && fileRelease == "" {
		return nil
	}
	target, err := releaseDate(options.TargetRelease)
	if err != nil {
		return err
	}
	options.TargetRelease = target
	if fileRelease > target {
		return fmt.Errorf("%w: XML %s новее выпуска %s", ErrReleaseOrder, fileRelease, target)
	}
	if options.Mode == "delta" {
		base, err := releaseDate(options.BaseRelease)
		if err != nil {
			return err
		}
		options.BaseRelease = base
		if len(options.Regions)+len(options.Kinds)+len(options.Sources) > 0 || options.LimitPerFile > 0 {
			return fmt.Errorf("%w: delta требует полного отбора", ErrReleaseOrder)
		}
	}
	return nil
}

// releaseJournal is optional for in-memory test adapters; the production journal implements it.
type releaseJournal interface {
	PrepareRelease(context.Context, string, string, string, string) error
	SetRunRelease(context.Context, int64, string, string) error
}
