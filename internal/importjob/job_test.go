package importjob_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"gar-importer/internal/gar"
	"gar-importer/internal/gar/record"
	"gar-importer/internal/importjob"
)

const houseDoc = `<?xml version="1.0" encoding="UTF-8"?><HOUSES>%s</HOUSES>`

func house(id int) string {
	return fmt.Sprintf(`<HOUSE ID="%d" OBJECTID="%d" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" HOUSENUM="%d" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="1"/>`, id, id, id)
}

// fakeArchive — источник из map: путь → содержимое.
type fakeArchive struct {
	files map[string]string
}

func (a *fakeArchive) Digest() (string, error) { return "fake-digest", nil }

func (a *fakeArchive) Names() []string {
	names := make([]string, 0, len(a.files))
	for name := range a.files {
		names = append(names, name)
	}
	return names
}

func (a *fakeArchive) Open(name string) (io.ReadCloser, error) {
	content, ok := a.files[name]
	if !ok {
		return nil, fmt.Errorf("нет файла %q", name)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (a *fakeArchive) Close() error { return nil }

// fakeWriter — writer одного формата: считает записи, ошибки вызывает по сценарию.
type fakeWriter struct {
	record.Handler // заглушки остальных форматов
	kind           gar.Kind
	records        atomic.Int64
	batchSize      int
	flushes        atomic.Int64
	writeErr       atomic.Int64 // после N записей возвращать ошибку
	flushErr       atomic.Bool  // ошибку возвращать на Save
	onCommit       func(committedInput, writeOps int64)
	skipped        int64
}

func (w *fakeWriter) House(ctx context.Context, r record.House) error {
	if w.writeErr.Load() > 0 && w.records.Load() >= w.writeErr.Load() {
		return fmt.Errorf("fake-ошибка записи")
	}
	w.records.Add(1)
	if int(w.records.Load())%w.batchSize == 0 {
		return w.Save(ctx)
	}
	return nil
}

func (w *fakeWriter) Save(ctx context.Context) error {
	w.flushes.Add(1)
	if w.flushErr.Load() {
		return fmt.Errorf("fake-ошибка коммита")
	}
	if w.onCommit != nil {
		w.onCommit(w.records.Load(), w.records.Load())
	}
	return nil
}

func (w *fakeWriter) Committed() int64                 { return w.records.Load() }
func (w *fakeWriter) WriteOps() int64                  { return w.records.Load() }
func (w *fakeWriter) SetOnCommit(f func(int64, int64)) { w.onCommit = f }
func (w *fakeWriter) SetSkip(n int64)                  { w.skipped = n }

// fakeFactory выдаёт fakeWriter для любого формата (в тестах — HOUSES).
type fakeFactory struct {
	writers           []*fakeWriter
	writeAfterRecords int64
}

func (f *fakeFactory) BeginFile(ctx context.Context, entry gar.Entry, batchSize int, baseCommitted, fileID int64) (importjob.Writer, error) {
	w := &fakeWriter{kind: entry.Kind, batchSize: batchSize}
	if f.writeAfterRecords > 0 {
		w.writeErr.Store(f.writeAfterRecords)
	}
	if baseCommitted > 0 {
		w.SetSkip(baseCommitted)
	}
	f.writers = append(f.writers, w)
	return w, nil
}

func TestJobSelectionAndLimit(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"gar_xml/77/AS_HOUSES_20261006_1.xml":      "<HOUSES>" + house(1) + house(2) + house(3) + "</HOUSES>",
		"gar_xml/78/AS_HOUSES_20261006_1.xml":      "<HOUSES>" + house(9) + "</HOUSES>",
		"gar_xml/AS_ADDR_OBJ_TYPES_20261006_1.xml": "<ADDRESSOBJECTTYPES></ADDRESSOBJECTTYPES>",
	}}
	factory := &fakeFactory{}
	report, err := importjob.Run(context.Background(), archive, factory, importjob.Options{
		BatchSize:    2,
		Regions:      []int{77},
		LimitPerFile: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Корневой справочник выбирается вместе с регионом 77; дом 78 отфильтрован.
	if len(report.Files) != 2 || report.Filtered != 1 || len(report.Skipped) != 0 {
		t.Fatalf("report: %+v", report)
	}
	var houses importjob.FileStats
	for _, file := range report.Files {
		if file.Kind == gar.KindHouses {
			houses = file
		}
	}
	if !houses.Limited || houses.Decoded != 2 {
		t.Fatalf("houses stats: %+v", houses)
	}
	housesRecords := int64(0)
	for _, writer := range factory.writers {
		if writer.kind == gar.KindHouses {
			housesRecords = writer.records.Load()
		}
	}
	if housesRecords != 2 {
		t.Fatalf("houses records: %d", housesRecords)
	}
}

func TestJobFlushOnBatchSize(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"77/AS_HOUSES_20261006_1.xml": "<HOUSES>" + house(1) + house(2) + house(3) + house(4) + house(5) + "</HOUSES>",
	}}
	factory := &fakeFactory{}
	report, err := importjob.Run(context.Background(), archive, factory, importjob.Options{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	// 5 записей: флаш на 2, 4 и финальный.
	if len(factory.writers) != 1 || factory.writers[0].flushes.Load() != 3 {
		t.Fatalf("flushes=%d", factory.writers[0].flushes.Load())
	}
	if report.Decoded != 5 || report.Written != 5 || report.CommittedInput != 5 {
		t.Fatalf("counters: %+v", report)
	}
}

func TestJobWriteErrorStopsAndKeepsCommitted(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"77/AS_HOUSES_20261006_1.xml": "<HOUSES>" + house(1) + house(2) + house(3) + house(4) + "</HOUSES>",
	}}
	factory := &fakeFactory{writeAfterRecords: 3}
	report, err := importjob.Run(context.Background(), archive, factory, importjob.Options{BatchSize: 2})
	if err == nil || !strings.Contains(err.Error(), "fake-ошибка записи") {
		t.Fatalf("want write error, got %v", err)
	}
	// Первые 2 записи подтверждены (записано), ошибка на третьей.
	if report.Written != 2 || report.Files[0].Done {
		t.Fatalf("report: %+v", report)
	}
}

func TestJobDecodeErrorBeforeWriter(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"77/AS_HOUSES_20261006_1.xml": "<HOUSES>" + house(1) + `<HOUSE ID="2" OBJECTID="2" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" OPERTYPEID="1" UPDATEDATE="2024-13-40" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="1"/>` + "</HOUSES>",
	}}
	factory := &fakeFactory{}
	report, err := importjob.Run(context.Background(), archive, factory, importjob.Options{BatchSize: 1})
	if err == nil {
		t.Fatal("want decode error")
	}
	var recordErr *gar.RecordError
	if !errors.As(err, &recordErr) || recordErr.Attribute != "UPDATEDATE" {
		t.Fatalf("want structured record error, got %v", err)
	}
	if factory.writers[0].records.Load() != 1 {
		t.Fatalf("records reached writer: %d", factory.writers[0].records.Load())
	}
	if report.Written != 1 {
		t.Fatalf("committed before error: %d", report.Written)
	}
}

func TestJobCommitError(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"77/AS_HOUSES_20261006_1.xml": "<HOUSES>" + house(1) + house(2) + "</HOUSES>",
	}}
	factory := &fakeFactory{}
	if _, err := importjob.Run(context.Background(), archive, factory, importjob.Options{BatchSize: 2}); err != nil {
		t.Fatal(err)
	}
	// Ошибка коммита: счётчики не продвигаются, ошибка возвращается.
	failing := &failingFactory{}
	if _, err := importjob.Run(context.Background(), archive, failing, importjob.Options{BatchSize: 2}); err == nil || !strings.Contains(err.Error(), "fake-ошибка коммита") {
		t.Fatalf("want commit error, got %v", err)
	}
	if failing.writer.flushes.Load() == 0 {
		t.Fatal("commit was not attempted")
	}
}

type failingFactory struct {
	writer *failingWriter
}

type failingWriter struct {
	fakeWriter
}

func (w *failingWriter) SetSkip(n int64) {}

func (f *failingFactory) BeginFile(ctx context.Context, entry gar.Entry, batchSize int, baseCommitted, fileID int64) (importjob.Writer, error) {
	f.writer = &failingWriter{fakeWriter{kind: entry.Kind, batchSize: batchSize}}
	f.writer.flushErr.Store(true)
	return f.writer, nil
}

func TestJobCancellation(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"77/AS_HOUSES_20261006_1.xml": "<HOUSES>" + house(1) + house(2) + house(3) + house(4) + "</HOUSES>",
	}}
	factory := &fakeFactory{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progressCalls := 0
	_, err := importjob.Run(ctx, archive, factory, importjob.Options{
		BatchSize: 1,
		Progress: func(stats importjob.FileStats) {
			progressCalls++
			if stats.Written > 0 {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if progressCalls < 2 {
		t.Fatalf("progress not reported: %d", progressCalls)
	}
}

func TestJobNoSelectedFiles(t *testing.T) {
	archive := &fakeArchive{files: map[string]string{
		"unexpected/AS_HOUSES_20261006_1.xml": "<HOUSES></HOUSES>",
	}}
	report, err := importjob.Run(context.Background(), archive, &fakeFactory{}, importjob.Options{})
	if err == nil {
		t.Fatal("want error for no selected files")
	}
	if len(report.Skipped) != 1 || !strings.Contains(report.Skipped[0].Reason, "не является кодом региона") {
		t.Fatalf("skipped: %+v", report.Skipped)
	}
}
