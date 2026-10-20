//go:build integration

package importjob_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gar-importer/internal/control"
	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
	"gar-importer/internal/input/zipgar"
	"gar-importer/internal/storage"
)

func TestReadyArchiveIsNotImportedAgain(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	archivePath := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_x.xml", `<HOUSES></HOUSES>`}})
	calls := 0
	run := func() importjob.Report {
		archive, err := zipgar.Open(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		report, err := importjob.RunWithJournal(ctx, archive, storage.NewWriterFactory(db.Client, journal), journal, importjob.Options{ResumeRunID: -1, ResumeOrNew: true, OnFileCompleted: func(context.Context, importjob.FileCompletedEvent) error { calls++; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	first, second := run(), run()
	if first.RunID != second.RunID || calls != 1 {
		t.Fatalf("duplicate imported: runs %d/%d, events %d", first.RunID, second.RunID, calls)
	}
}

func TestReleaseChainAndOldSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	run := func(date, mode, base string) error {
		archivePath := makeZIP(t, []archiveFile{{"77/AS_HOUSES_" + date + "_x.xml", `<HOUSES></HOUSES>`}})
		_, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), storage.NewWriterFactory(db.Client, journal), journal, importjob.Options{Mode: mode, BaseRelease: base})
		return err
	}
	if err := run("20261001", "snapshot", ""); err != nil {
		t.Fatal(err)
	}
	if err := run("20261003", "delta", "20261002"); !errors.Is(err, importjob.ErrReleaseOrder) {
		t.Fatalf("gap accepted: %v", err)
	}
	if err := run("20261002", "delta", "20261001"); err != nil {
		t.Fatal(err)
	}
	if err := run("20261003", "delta", "20261002"); err != nil {
		t.Fatal(err)
	}
	if err := run("20261002", "delta", "20261001"); !errors.Is(err, importjob.ErrReleaseOrder) {
		t.Fatalf("D2 then D1 accepted: %v", err)
	}
	if err := run("20261001", "snapshot", ""); !errors.Is(err, importjob.ErrReleaseOrder) {
		t.Fatalf("old full accepted: %v", err)
	}
}

func TestUpsertRejectsOlderRow(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	run := func(row string) {
		archivePath := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_x.xml", document(gar.KindHouses, row)}})
		_, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), storage.NewWriterFactory(db.Client, journal), journal, importjob.Options{})
		if err != nil {
			t.Fatal(err)
		}
	}
	newer := strings.ReplaceAll(houseRecord(1), `UPDATEDATE="2024-11-23"`, `UPDATEDATE="2026-10-01"`)
	run(newer)
	run(houseRecord(1))
	row, err := db.Client.House.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(row.UpdateDate) != "2026-10-01" {
		t.Fatalf("old row overwrote new: %s", row.UpdateDate)
	}
}

func TestPartialSnapshotAlsoRejectsOlderArchive(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	run := func(date string, regions []int) error {
		archivePath := makeZIP(t, []archiveFile{{"77/AS_HOUSES_" + date + "_x.xml", `<HOUSES></HOUSES>`}})
		_, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), storage.NewWriterFactory(db.Client, journal), journal, importjob.Options{Regions: regions})
		return err
	}
	if err := run("20261003", []int{77}); err != nil {
		t.Fatal(err)
	}
	if err := run("20261002", nil); !errors.Is(err, importjob.ErrReleaseOrder) {
		t.Fatalf("old full bypassed newer regional import: %v", err)
	}
	var applied string
	if err := db.SQL().QueryRowContext(ctx, `SELECT COALESCE(applied_release,'') FROM gar_control.datasets`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != "" {
		t.Fatalf("partial import bootstrapped full dataset: %s", applied)
	}
}
