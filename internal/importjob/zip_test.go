//go:build integration

package importjob_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gar-importer/ent"
	"gar-importer/ent/param"
	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/gar"
	"gar-importer/internal/gar/record"
	"gar-importer/internal/importjob"
	"gar-importer/internal/input/zipgar"
	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
)

func testDB(t *testing.T, ctx context.Context) *storage.Store {
	t.Helper()
	databaseURL := os.Getenv("GAR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set GAR_TEST_DATABASE_URL to an empty test database")
	}
	db, err := storage.Open(ctx, config.Config{
		DatabaseURL:    databaseURL,
		MaxOpenConns:   4,
		MaxIdleConns:   4,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// resetSchema применяет версионированные миграции к чистой схеме данных.
func resetSchema(t *testing.T, ctx context.Context, db *storage.Store) {
	t.Helper()
	if _, err := db.Exec(ctx, "DROP SCHEMA IF EXISTS "+migrations.DataSchema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "DROP SCHEMA IF EXISTS "+migrations.ControlSchema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewRunner(db.SQL(), "../../migrations/atlas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestZIPToAllTables(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := testDB(t, ctx)
	// Durability-проверки требуют реальных коммитов: схема сбрасывается,
	// откатываемая транзакция не используется.
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	factory := storage.NewWriterFactory(db.Client, journal)
	data, err := os.ReadFile("../gar/record/testdata/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Kind       gar.Kind
		XML, Model string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	var files []archiveFile
	var paramXML, houseXML string
	for _, fixture := range fixtures {
		prefix, folder, record := string(fixture.Kind), "gar_xml/77/", fixture.XML
		if strings.HasSuffix(prefix, "_TYPES") || strings.HasSuffix(prefix, "_KINDS") || fixture.Kind == gar.KindObjectLevels {
			folder = "gar_xml/"
		}
		if fixture.Kind == gar.KindParam {
			prefix, paramXML = "AS_ADDR_OBJ_PARAMS", record
		}
		if fixture.Kind == gar.KindHouses {
			houseXML = record
			record += strings.ReplaceAll(record, `HOUSENUM="1"`, `HOUSENUM="last"`)
		}
		files = append(files, archiveFile{folder + prefix + "_20261006_x.xml", document(fixture.Kind, record)})
	}
	for _, prefix := range []string{"AS_HOUSES_PARAMS", "AS_APARTMENTS_PARAMS", "AS_ROOMS_PARAMS", "AS_STEADS_PARAMS", "AS_CARPLACES_PARAMS"} {
		files = append(files, archiveFile{"gar_xml/77/" + prefix + "_20261006_x.xml", document(gar.KindParam, paramXML)})
	}
	files = append(files,
		archiveFile{"gar_xml/78/AS_HOUSES_20261006_x.xml", document(gar.KindHouses, houseXML)},
		archiveFile{"README.txt", "test archive"},
		archiveFile{"AS_UNKNOWN_20261006_x.xml", "unsupported file is reported"})
	archive := makeZIP(t, files)
	options := importjob.Options{BatchSize: 2, Regions: []int{77}, ResumeRunID: -1, ResumeOrNew: true}
	for run := 1; run <= 2; run++ {
		report, err := importjob.RunWithJournal(ctx, mustOpen(t, archive), factory, journal, options)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Files) != 28 || report.Decoded != 29 || report.Written != 28 || report.Filtered != 1 || len(report.Skipped) != 1 || report.Ignored != 1 {
			t.Fatalf("run %d: unexpected report %+v", run, report)
		}
		for _, fixture := range fixtures {
			entity := reflect.ValueOf(db.Client).Elem().FieldByName(fixture.Model)
			query := entity.MethodByName("Query").Call(nil)[0]
			result := query.MethodByName("Count").Call([]reflect.Value{reflect.ValueOf(ctx)})
			want := int64(1)
			if fixture.Kind == gar.KindParam {
				want = 6
			}
			if !result[1].IsNil() || result[0].Int() != want {
				t.Fatalf("%s after run %d: count=%v, error=%v", fixture.Model, run, result[0], result[1])
			}
		}
	}
	parameter, err := db.Client.Param.Query().Where(param.SourceEQ("AS_HOUSES_PARAMS"), param.SourceIDEQ(1)).Only(ctx)
	if err != nil || parameter.SourceID != 1 {
		t.Fatalf("parameter source: %v, %v", parameter, err)
	}
	house, err := db.Client.House.Get(ctx, 1)
	if err != nil || house.HouseNum == nil || *house.HouseNum != "last" {
		t.Fatalf("duplicate ID in batch: %v, %v", house, err)
	}

	// Новый документ должен очистить старые необязательные значения и сохранить false.
	updated := `<HOUSE ID="1" OBJECTID="1" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" HOUSENUM="updated" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="0" ISACTIVE="0"/>`
	second := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_y.XML", document(gar.KindHouses, updated)}})
	if _, err := importjob.RunWithJournal(ctx, mustOpen(t, second), factory, journal, options); err != nil {
		t.Fatal(err)
	}
	house, err = db.Client.House.Get(ctx, 1)
	if err != nil || house.AddNum1 != nil || house.HouseType != nil || house.PrevID != nil || house.IsActive || house.IsActual || *house.HouseNum != "updated" {
		t.Fatalf("upsert did not replace NULL/false: %v, %v", house, err)
	}
	options.Kinds = []gar.Kind{gar.KindHouses}
	report, err := importjob.RunWithJournal(ctx, mustOpen(t, archive), factory, journal, options)
	if err != nil || len(report.Files) != 1 || report.Filtered != 28 {
		t.Fatalf("format filter: %+v, %v", report, err)
	}

	// Лимит действует отдельно на файл и сохраняет последний неполный пакет.
	rows := strings.ReplaceAll(houseXML, `ID="1" OBJECTID`, `ID="3" OBJECTID`) + strings.ReplaceAll(houseXML, `ID="1" OBJECTID`, `ID="4" OBJECTID`) + strings.ReplaceAll(houseXML, `ID="1" OBJECTID`, `ID="5" OBJECTID`)
	sample := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_z.xml", document(gar.KindHouses, rows)}})
	options.LimitPerFile, options.BatchSize = 2, 3
	report, err = importjob.RunWithJournal(ctx, mustOpen(t, sample), factory, journal, options)
	if err != nil || report.Written != 2 || !report.Files[0].Limited {
		t.Fatalf("sample: %+v, %v", report, err)
	}
	if exists, err := db.Client.House.Query().Where().Count(ctx); err != nil || exists != 3 {
		t.Fatalf("sample count=%d, %v", exists, err)
	}

	// При повреждённом XML возвращается ошибка и счётчик уже сохранённых пакетов.
	broken := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_bad.xml", "<HOUSES>" + strings.ReplaceAll(houseXML, `ID="1" OBJECTID`, `ID="6" OBJECTID`) + "<HOUSE"}})
	options.LimitPerFile, options.BatchSize = 0, 1
	report, err = importjob.RunWithJournal(ctx, mustOpen(t, broken), factory, journal, options)
	if err == nil || report.Written != 1 || report.Files[0].Done {
		t.Fatalf("broken XML: %+v, %v", report, err)
	}

	stopCtx, stop := context.WithCancel(ctx)
	options.Progress = func(stats importjob.FileStats) {
		if stats.Written > 0 {
			stop()
		}
	}
	report, err = importjob.RunWithJournal(stopCtx, mustOpen(t, sample), factory, journal, options)
	if !errors.Is(err, context.Canceled) || report.Written != 1 {
		t.Fatalf("cancel: %+v, %v", report, err)
	}
}

// Сценарий этапа 6: реальный коммит → «сбой» процесса → возобновление без
// пропусков; подмена архива/options отвергается; второй writer не пишет параллельно.
func TestJournalResumeAndLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	journal, err := control.Open(ctx, db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	factory := storage.NewWriterFactory(db.Client, journal)
	records := make([]string, 200)
	for i := range records {
		records[i] = houseRecord(i + 1)
	}
	archivePath := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_1.xml", document(gar.KindHouses, strings.Join(records, ""))}})

	// Прерываем после четырёх коммитов: callback обязан идти после COMMIT,
	// поэтому синхронная отмена не откатывает уже подтверждённый пакет.
	runCtx, stop := context.WithCancel(ctx)
	options := importjob.Options{BatchSize: 2, Regions: []int{77}, ResumeRunID: -1, ResumeOrNew: true}
	var stopOnce bool
	options.Progress = func(stats importjob.FileStats) {
		if stats.CommittedInput >= 8 && !stopOnce {
			stopOnce = true
			stop()
		}
	}
	report, err := importjob.RunWithJournal(runCtx, mustOpen(t, archivePath), factory, journal, options)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run: %v", err)
	}
	runID := report.RunID
	if runID == 0 {
		t.Fatal("no run id in journal")
	}
	var state string
	var lastCommitted int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT state FROM gar_control.import_runs WHERE id = $1`, runID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "canceled" {
		t.Fatalf("run state: %s", state)
	}
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT last_committed_record FROM gar_control.import_files WHERE run_id = $1`, runID).Scan(&lastCommitted); err != nil {
		t.Fatal(err)
	}
	var persisted int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT count(*) FROM gar.houses`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if lastCommitted < 8 || lastCommitted != persisted {
		t.Fatalf("checkpoint=%d persisted=%d; expected cumulative committed records", lastCommitted, persisted)
	}

	archive := mustOpen(t, archivePath)
	digest, err := archive.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidateState := range []string{"failed", "loading", "planned", "canceled"} {
		if _, err := db.Exec(ctx, `UPDATE gar_control.import_runs SET state=$2 WHERE id=$1`, runID, candidateState); err != nil {
			t.Fatal(err)
		}
		candidate, err := journal.LatestResumableRun(ctx, digest)
		if err != nil || candidate != runID {
			t.Fatalf("state %s: candidate=%d error=%v", candidateState, candidate, err)
		}
	}

	// Подмена архива (тот же путь, другой digest) отвергается.
	tampered := makeZIP(t, []archiveFile{{"77/AS_HOUSES_20261006_1.xml", document(gar.KindHouses, strings.Join(records[:4], ""))}})
	if _, err := importjob.RunWithJournal(ctx, mustOpen(t, tampered), factory, journal, importjob.Options{
		BatchSize: 2, Regions: []int{77}, ResumeRunID: runID,
	}); err == nil || !strings.Contains(err.Error(), "изменился digest") {
		t.Fatalf("tampered resume: %v", err)
	}

	// Параметры исходного запуска менять нельзя.
	for _, changed := range []importjob.Options{
		{BatchSize: 2, Regions: []int{50}, ResumeRunID: runID},
		{BatchSize: 2, Regions: []int{77}, Kinds: []gar.Kind{gar.KindHouses}, ResumeRunID: runID},
		{BatchSize: 2, Regions: []int{77}, Sources: []string{"AS_HOUSES_PARAMS"}, ResumeRunID: runID},
	} {
		if _, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), factory, journal, changed); err == nil || !strings.Contains(err.Error(), "параметры") {
			t.Fatalf("changed selection accepted: %+v: %v", changed, err)
		}
	}
	// Возобновление тем же архивом: подтверждённые записи не пишутся второй раз.
	resumeReport, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), factory, journal, importjob.Options{
		BatchSize: 2, Regions: []int{77}, ResumeRunID: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumeReport.Written != int64(len(records))-lastCommitted {
		t.Fatalf("resume wrote %d, expected %d", resumeReport.Written, int64(len(records))-lastCommitted)
	}
	if resumeReport.RunID != runID {
		t.Fatalf("resumed a different run: %d != %d", resumeReport.RunID, runID)
	}
	if !resumeReport.Files[0].Resumed {
		t.Fatalf("expected resumed file: %+v", resumeReport.Files[0])
	}
	var houses int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT count(*) FROM gar.houses`).Scan(&houses); err != nil {
		t.Fatal(err)
	}
	if houses != int64(len(records)) {
		t.Fatalf("houses after resume: %d, want %d (без пропусков и дублей)", houses, len(records))
	}
	var stateAfter string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT state FROM gar_control.import_files WHERE run_id = $1`, runID).Scan(&stateAfter); err != nil {
		t.Fatal(err)
	}
	if stateAfter != "complete" {
		t.Fatalf("file state after resume: %s", stateAfter)
	}

	// Повторный resume: файл complete — пропускается.
	skipReport, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), factory, journal, importjob.Options{
		BatchSize: 2, Regions: []int{77}, ResumeRunID: runID,
	})
	if err != nil || !skipReport.Files[0].SkippedComplete {
		t.Fatalf("complete file not skipped: %+v, %v", skipReport.Files[0], err)
	}

	// Смена режима при resume отвергается.
	if _, err := importjob.RunWithJournal(ctx, mustOpen(t, archivePath), factory, journal, importjob.Options{
		BatchSize: 2, Regions: []int{77}, LimitPerFile: 1, ResumeRunID: runID,
	}); err == nil || !strings.Contains(err.Error(), "изменился режим") {
		t.Fatalf("mode change resume: %v", err)
	}

	// Второй writer не пишет в dataset параллельно.
	release, err := journal.AcquireDataWriteLock(ctx, "gar")
	if err != nil {
		t.Fatal(err)
	}
	_, err = importjob.RunWithJournal(ctx, mustOpen(t, archivePath), storage.NewWriterFactory(db.Client, journal), journal, importjob.Options{BatchSize: 2, Regions: []int{77}})
	if err == nil || !strings.Contains(err.Error(), "другим процессом") {
		t.Fatalf("second writer: %v", err)
	}
	release()
}

func houseRecord(id int) string {
	return fmt.Sprintf(`<HOUSE ID="%d" OBJECTID="%d" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" HOUSENUM="%d" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTUAL="1" ISACTIVE="1"/>`, id, id, id)
}

type archiveFile struct{ name, content string }

func makeZIP(t *testing.T, files []archiveFile) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "gar.zip")
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, source := range files {
		entry, err := writer.Create(source.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(source.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func document(kind gar.Kind, records string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><` + kind.RootElement() + `>` + records + `</` + kind.RootElement() + `>`
}

func mustOpen(t *testing.T, path string) *zipgar.Archive {
	t.Helper()
	archive, err := zipgar.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestBatchCountersOnlyAfterCommit(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetSchema(t, ctx, db)
	// Deferred constraint fails only at COMMIT, after bulk and checkpoint succeeded.
	_, err := db.Exec(ctx, `CREATE FUNCTION gar.reject_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject commit'; END $$;
 CREATE CONSTRAINT TRIGGER reject_commit AFTER INSERT ON gar.houses DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION gar.reject_commit()`)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := ent.NewGARBatch(db.Client, gar.KindHouses, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	batch.SetOnCommit(func(int64, int64) { callbacks++ })
	_, err = record.DecodeStream(ctx, strings.NewReader(document(gar.KindHouses, houseRecord(1))), gar.KindHouses, 0, batch)
	if err == nil {
		t.Fatal("expected deferred commit failure")
	}
	if batch.Committed() != 0 || batch.WriteOps() != 0 || callbacks != 0 || batch.Len() != 1 {
		t.Fatalf("failed commit advanced state: committed=%d writes=%d callbacks=%d pending=%d", batch.Committed(), batch.WriteOps(), callbacks, batch.Len())
	}
	if _, err := db.Exec(ctx, `DROP TRIGGER reject_commit ON gar.houses`); err != nil {
		t.Fatal(err)
	}
	if err := batch.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if batch.Committed() != 1 || batch.WriteOps() != 1 || callbacks != 1 || batch.Len() != 0 {
		t.Fatal("pending batch was not recovered after failed commit")
	}

}
