// Package importjob — сценарий импорта архива ГАР: план архива, выбор файлов,
// поток записей, пакетная запись, прогресс и отчёт. Сценарий не знает о ZIP,
// Ent и PostgreSQL: источники, writer и журнал подключаются через порты.
package importjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"strings"
	"time"

	"gar-importer/internal/gar"
	"gar-importer/internal/gar/record"
)

const (
	// DefaultBatchSize — записей в пакете по умолчанию.
	DefaultBatchSize = 1000
	// MaxBatchSize ограничен параметрами PostgreSQL для bulk-вставки.
	MaxBatchSize = 2000
	// DefaultSchema — схема данных по умолчанию (legacy dataset).
	DefaultSchema = "gar"
)

var ErrResumeMismatch = errors.New("возобновление отклонено")

var ErrNoSelectedFiles = errors.New("в архиве нет выбранных XML-файлов ГАР")

// Режимы и состояния запуска (значения совпадают с internal/control).
const (
	ModeSnapshot     = "snapshot"
	ModeSample       = "sample"
	RunStateReady    = "ready"
	RunStateSampled  = "sampled"
	RunStateFailed   = "failed"
	RunStateCanceled = "canceled"
)

// Archive — источник XML-файлов архива (порт; реализует zipgar).
type Archive interface {
	// Names возвращает пути XML-файлов в детерминированном порядке.
	Names() []string
	// Open открывает файл для чтения; вызывающий закрывает результат.
	Open(name string) (io.ReadCloser, error)
	// Digest — криптографический digest неизменяемого содержимого источника.
	Digest() (string, error)
	Close() error
}

// Writer — пакетная запись проверенных записей одного формата (порт;
// реализует адаптер Ent/PostgreSQL).
type Writer interface {
	record.Handler
	// Save досылает текущий пакет и фиксирует данные вместе с checkpoint.
	Save(ctx context.Context) error
	// Committed — подтверждённо записанные входные записи (включая дубликаты).
	Committed() int64
	// WriteOps — дедуплицированные операции записи (insert/update).
	WriteOps() int64
	// SetOnCommit вызывается после каждого успешного коммита пакета.
	SetOnCommit(func(committedInput, writeOps int64))
	// SetSkip помечает первые n записей файла как подтверждённые ранее
	// (resume): они валидируются, но не пишутся.
	SetSkip(n int64)
}

// WriterFactory создаёт writer для одного файла плана; baseCommitted —
// подтверждённый checkpoint предыдущего запуска (0 при новом файле),
// fileID — строка журнала для атомарного checkpoint (0 без журнала).
type WriterFactory interface {
	BeginFile(ctx context.Context, entry gar.Entry, batchSize int, baseCommitted, fileID int64) (Writer, error)
}

// Journal — журнал выполнения импорта (порт; реализует internal/control).
type Journal interface {
	StartRun(ctx context.Context, schemaName, mode, sourcePath, sourceDigest, programVersion, options string) (int64, error)
	StartFile(ctx context.Context, runID int64, entryPath string, entryIndex int, kind, source string, region int, entryDate, entrySequence string) (fileID, baseCommitted int64, err error)
	CompleteFiles(ctx context.Context, runID int64) (map[string]int64, error)
	RunSource(ctx context.Context, runID int64) (digest, mode string, err error)
	RunOptions(ctx context.Context, runID int64) (string, error)
	ResumeRun(ctx context.Context, runID int64) error
	// LatestResumableRun возвращает последний незавершённый запуск с этим digest (0 — нет).
	LatestResumableRun(ctx context.Context, digest string) (int64, error)
	FinishFile(ctx context.Context, fileID, writeOps int64, integrityChecked bool, runErr error) error
	FinishRun(ctx context.Context, runID int64, state string, runErr error) error
	AcquireDataWriteLock(ctx context.Context, schemaName string) (release func(), err error)
}

// Options — параметры запуска сценария.
type Options struct {
	BatchSize      int
	LimitPerFile   int64
	Regions        []int
	Kinds          []gar.Kind
	Sources        []string // точные семейства параметров, например AS_HOUSES_PARAMS
	Progress       func(FileStats)
	ProgramVersion string
	// OnFileCompleted — необязательный хук завершения файла (например, публикация
	// события в шину). Вызывается после успешного коммита данных (writer.Save),
	// но до фиксации файла завершённым в журнале: ошибка хука помечает файл
	// failed, и повторный запуск дообработает его (upsert идемпотентен).
	// nil — хук пропускается.
	OnFileCompleted func(ctx context.Context, event FileCompletedEvent) error
	// ResumeRunID: >0 — продолжить указанный запуск; -1 — продолжить последний
	// незавершённый с тем же digest; 0 — новый запуск. При resume проверяются
	// digest, режим и параметры отбора.
	ResumeRunID int64
	// ResumeOrNew permits a fresh run when automatic resume has no candidate.
	ResumeOrNew  bool
	sourceDigest string
}

// FileCompletedEvent — событие успешной обработки одного файла плана:
// данные закоммичены, целостность файла подтверждена полным чтением.
type FileCompletedEvent struct {
	RunID       int64
	Digest      string
	Kind        gar.Kind
	Source      string // нормализованное семейство параметров (для PARAM)
	Region      int    // 0 — общероссийский справочник
	Path        string
	Written     int64 // дедуплицированные операции записи (insert/update)
	CompletedAt time.Time
}

// FileStats — прогресс по файлу.
type FileStats struct {
	Name             string
	Kind             gar.Kind
	Region           int
	Decoded, Written int64
	CommittedInput   int64
	Limited, Done    bool
	Resumed          bool // файл продолжён с checkpoint предыдущего запуска
	SkippedComplete  bool // файл уже был полностью обработан ранее
	Duration         time.Duration
}

// Report — итоги запуска.
type Report struct {
	Files             []FileStats
	Skipped           []SkippedFile
	Filtered, Ignored int
	Decoded, Written  int64
	CommittedInput    int64
	RunID             int64
	Duration          time.Duration
}

// SkippedFile — файл, не попавший в обработку, с причиной.
type SkippedFile struct{ Name, Reason string }

// Run — запуск без журнала (совместимость и unit-тесты с fake writer).
func Run(ctx context.Context, source Archive, writers WriterFactory, options Options) (Report, error) {
	return run(ctx, source, writers, nil, options)
}

// RunWithJournal — запуск с журналом (gar_control): digest источника, режим,
// checkpoint файлов, возобновление и блокировка параллельной записи.
func RunWithJournal(ctx context.Context, source Archive, writers WriterFactory, journal Journal, options Options) (Report, error) {
	return run(ctx, source, writers, journal, options)
}

func run(ctx context.Context, source Archive, writers WriterFactory, journal Journal, options Options) (report Report, err error) {
	started := time.Now()
	defer func() { report.Duration = time.Since(started) }()
	defer source.Close()
	if options.BatchSize == 0 {
		options.BatchSize = DefaultBatchSize
	}
	if err := ValidateOptions(options); err != nil {
		return report, err
	}
	filters, err := BuildFilters(options)
	if err != nil {
		return report, err
	}

	digest, err := source.Digest()
	if err != nil {
		return report, err
	}
	options.sourceDigest = digest
	var (
		runID       int64
		complete    map[string]int64
		releaseLock func()
	)
	if journal != nil {
		if releaseLock, err = journal.AcquireDataWriteLock(ctx, DefaultSchema); err != nil {
			return report, err
		}
		defer releaseLock()
		if options.ResumeOrNew && options.LimitPerFile == 0 {
			if ready, ok := journal.(interface {
				ReadyRun(context.Context, string, Options) (int64, error)
			}); ok {
				id, readyErr := ready.ReadyRun(ctx, digest, options)
				if readyErr != nil {
					return report, readyErr
				}
				if id != 0 {
					report.RunID = id
					return report, nil
				}
			}
		}
		runID, err = beginRun(ctx, journal, source, digest, options)
		if err != nil {
			return report, err
		}
		report.RunID = runID
		if complete, err = journal.CompleteFiles(ctx, runID); err != nil {
			return finishRunErr(ctx, journal, runID, report, err)
		}
	}
	var runErr error
	defer func() {
		if journal != nil {
			state := RunStateReady
			switch {
			case runErr == nil && options.LimitPerFile > 0:
				state = RunStateSampled // проба не разрешает публикацию
			case errors.Is(runErr, context.Canceled):
				state = RunStateCanceled
			case runErr != nil:
				state = RunStateFailed
			}
			if ferr := journal.FinishRun(ctx, runID, state, runErr); ferr != nil && runErr == nil {
				runErr, err = ferr, ferr
			}
		}
	}()

	var xmlNames []string
	for _, name := range source.Names() {
		if strings.EqualFold(path.Ext(name), ".xml") {
			xmlNames = append(xmlNames, name)
		} else {
			report.Ignored++
		}
	}
	plan := gar.Plan(xmlNames)
	selected := gar.Select(plan, filters)
	for _, entry := range gar.Unknown(plan) {
		report.Skipped = append(report.Skipped, SkippedFile{Name: entry.Path, Reason: entry.Reason})
	}
	for i := range plan {
		if plan[i].Status == gar.StatusFiltered {
			report.Filtered++
		}
	}
	for _, entry := range plan {
		if err := ctx.Err(); err != nil {
			runErr = err
			return report, err
		}
		if entry.Status != gar.StatusSelected {
			continue
		}
		if _, done := complete[entry.Path]; done {
			// Завершённые файлы пропускаются при возобновлении.
			report.Files = append(report.Files, FileStats{
				Name: entry.Path, Kind: entry.Kind, Region: entry.Region,
				Done: true, SkippedComplete: true,
			})
			continue
		}
		stats, fileErr := importFile(ctx, source, writers, journal, runID, entry, options)
		report.Files = append(report.Files, stats)
		report.Decoded += stats.Decoded
		report.Written += stats.Written
		report.CommittedInput += stats.CommittedInput
		if fileErr != nil {
			runErr = fileErr
			return report, fmt.Errorf("%s: %w", entry.Path, fileErr)
		}
	}
	if selected == 0 {
		runErr = ErrNoSelectedFiles
		return report, runErr
	}
	return report, nil
}

// beginRun создаёт запуск или проверяет совместимость при resume.
func beginRun(ctx context.Context, journal Journal, source Archive, digest string, options Options) (int64, error) {
	mode := ModeSnapshot
	if options.LimitPerFile > 0 {
		mode = ModeSample
	}
	if options.ResumeRunID == -1 {
		resumeID, err := journal.LatestResumableRun(ctx, digest)
		if err != nil {
			return 0, err
		}
		if resumeID == 0 && !options.ResumeOrNew {
			return 0, fmt.Errorf("возобновление: нет незавершённого запуска с этим digest источника")
		}
		options.ResumeRunID = resumeID
	}
	if options.ResumeRunID > 0 {
		runDigest, runMode, err := journal.RunSource(ctx, options.ResumeRunID)
		if err != nil {
			return 0, err
		}
		if runDigest != digest {
			return 0, fmt.Errorf("%w: изменился digest источника: в запуске %s, в архиве %s", ErrResumeMismatch, runDigest, digest)
		}
		if runMode != mode {
			return 0, fmt.Errorf("%w: изменился режим: в запуске %s, запрошен %s", ErrResumeMismatch, runMode, mode)
		}
		savedOptions, err := journal.RunOptions(ctx, options.ResumeRunID)
		if err != nil {
			return 0, err
		}
		if err := validateResumeOptions(savedOptions, options); err != nil {
			return 0, err
		}
		if err := journal.ResumeRun(ctx, options.ResumeRunID); err != nil {
			return 0, err
		}
		return options.ResumeRunID, nil
	}
	optionsJSON, err := json.Marshal(map[string]any{
		"batch_size": options.BatchSize, "limit_per_file": options.LimitPerFile,
		"regions": options.Regions, "kinds": options.Kinds, "sources": options.Sources,
	})
	if err != nil {
		return 0, err
	}
	programVersion := options.ProgramVersion
	if programVersion == "" {
		programVersion = "dev"
	}
	return journal.StartRun(ctx, DefaultSchema, mode, sourcePath(source), digest, programVersion, string(optionsJSON))
}

func sourcePath(source Archive) string {
	if describer, ok := source.(interface{ Path() string }); ok {
		return describer.Path()
	}
	return ""
}

// importFile обрабатывает один файл: checkpoint, поток записей, финальный пакет.
func importFile(ctx context.Context, source Archive, writers WriterFactory, journal Journal, runID int64, entry gar.Entry, options Options) (stats FileStats, err error) {
	started := time.Now()
	stats = FileStats{Name: entry.Path, Kind: entry.Kind, Region: entry.Region}
	defer func() { stats.Duration = time.Since(started) }()
	reader, err := source.Open(entry.Path)
	if err != nil {
		return stats, err
	}
	defer reader.Close()

	var (
		fileID int64
		base   int64
	)
	if journal != nil {
		if fileID, base, err = journal.StartFile(ctx, runID, entry.Path, entry.Index,
			string(entry.Kind), entry.Source, entry.Region, entry.Date, entry.Sequence); err != nil {
			return stats, err
		}
		if base > 0 {
			stats.Resumed = true
		}
	}
	writer, err := writers.BeginFile(ctx, entry, options.BatchSize, base, fileID)
	if err != nil {
		return stats, err
	}
	if base > 0 {
		writer.SetSkip(base)
	}
	progress := func() {
		stats.Duration = time.Since(started)
		if options.Progress != nil {
			options.Progress(stats)
		}
	}
	progress()
	writer.SetOnCommit(func(committedInput, writeOps int64) {
		stats.CommittedInput = base + committedInput
		stats.Written = writeOps
		progress()
	})
	result, err := record.DecodeStream(ctx, reader, entry.Kind, options.LimitPerFile, writer)
	if err != nil {
		if journal != nil {
			_ = journal.FinishFile(context.WithoutCancel(ctx), fileID, writer.WriteOps(), false, err)
		}
		return stats, err
	}
	stats.Decoded = result.Records
	stats.Limited = result.Limited
	// Полное чтение до EOF подтверждает целостность (CRC проверяет архиватор);
	// проба (limit) — не подтверждает.
	if err := writer.Save(ctx); err != nil {
		if journal != nil {
			_ = journal.FinishFile(context.WithoutCancel(ctx), fileID, writer.WriteOps(), false, err)
		}
		return stats, err
	}
	// Хук завершения вызывается до фиксации файла в журнале: ошибка хука
	// (например, недоступность шины событий) оставляет файл незавершённым,
	// и повторный запуск дообработает его с checkpoint.
	if options.OnFileCompleted != nil {
		event := FileCompletedEvent{
			RunID: runID, Kind: entry.Kind, Source: entry.Source, Region: entry.Region,
			Digest: options.sourceDigest, Path: entry.Path, Written: writer.WriteOps(), CompletedAt: time.Now(),
		}
		if err := options.OnFileCompleted(ctx, event); err != nil {
			if journal != nil {
				_ = journal.FinishFile(context.WithoutCancel(ctx), fileID, writer.WriteOps(), false, err)
			}
			return stats, err
		}
	}
	if journal != nil {
		if err := journal.FinishFile(ctx, fileID, writer.WriteOps(), !result.Limited, nil); err != nil {
			return stats, err
		}
	}
	stats.Done = true
	progress()
	return stats, nil
}

func finishRunErr(ctx context.Context, journal Journal, runID int64, report Report, runErr error) (Report, error) {
	_ = journal.FinishRun(ctx, runID, RunStateFailed, runErr)
	return report, runErr
}

// ValidateOptions проверяет параметры сценария: вызывается до открытия БД.
func ValidateOptions(options Options) error {
	// 0 — значение по умолчанию; иная семантика не вводится.
	if options.BatchSize != 0 && (options.BatchSize < 1 || options.BatchSize > MaxBatchSize) {
		return fmt.Errorf("размер пакета должен быть от 1 до %d", MaxBatchSize)
	}
	if options.LimitPerFile < 0 {
		return fmt.Errorf("лимит на файл не может быть отрицательным")
	}
	return nil
}

// BuildFilters собирает фильтры плана из опций; проверка значений происходит
// до открытия БД и архива.
func BuildFilters(options Options) (gar.Filters, error) {
	filters := gar.Filters{
		Regions: make(map[int]bool, len(options.Regions)),
		Kinds:   make(map[gar.Kind]bool, len(options.Kinds)),
		Sources: make(map[string]bool, len(options.Sources)),
	}
	for _, region := range options.Regions {
		if region < 1 || region > 99 {
			return filters, fmt.Errorf("регион должен быть от 1 до 99: %d", region)
		}
		filters.Regions[region] = true
	}
	for _, kind := range options.Kinds {
		if !kind.Known() {
			return filters, fmt.Errorf("неподдерживаемый формат ГАР %q", kind)
		}
		filters.Kinds[kind] = true
	}
	for _, source := range options.Sources {
		if !gar.IsParameterPrefix(source) {
			return filters, fmt.Errorf("неизвестное семейство параметров %q", source)
		}
		filters.Sources[gar.Source(source)] = true
	}
	return filters, nil
}

func validateResumeOptions(saved string, requested Options) error {
	var previous struct {
		Regions      []int      `json:"regions"`
		Kinds        []gar.Kind `json:"kinds"`
		Sources      []string   `json:"sources"`
		LimitPerFile int64      `json:"limit_per_file"`
	}
	if err := json.Unmarshal([]byte(saved), &previous); err != nil {
		return fmt.Errorf("возобновление: параметры исходного запуска: %w", err)
	}
	oldFilters, err := BuildFilters(Options{Regions: previous.Regions, Kinds: previous.Kinds, Sources: previous.Sources})
	if err != nil {
		return err
	}
	newFilters, err := BuildFilters(requested)
	if err != nil {
		return err
	}
	if previous.LimitPerFile != requested.LimitPerFile || !maps.Equal(oldFilters.Regions, newFilters.Regions) || !maps.Equal(oldFilters.Kinds, newFilters.Kinds) || !maps.Equal(oldFilters.Sources, newFilters.Sources) {
		return fmt.Errorf("%w: изменились параметры Regions/Kinds/Sources/LimitPerFile", ErrResumeMismatch)
	}
	return nil
}

// MatchesSelection compares the data selection independently of operational settings.
func MatchesSelection(saved string, options Options) bool {
	return validateResumeOptions(saved, options) == nil
}
