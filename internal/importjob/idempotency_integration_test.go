//go:build integration

package importjob_test

import (
	"context"
	"testing"

	"gar-importer/internal/control"
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
