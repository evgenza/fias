//go:build integration

package worker_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"gar-importer/internal/config"
	"gar-importer/internal/events"
	"gar-importer/internal/gar"
	"gar-importer/internal/objectstorage"
	"gar-importer/internal/storage"
	"gar-importer/internal/worker"
)

// Образы пинуются для воспроизводимости (как в тестах events и objectstorage).
const (
	postgresImage = "postgres:18.6-alpine"
	natsImage     = "nats:2.10-alpine"
	minioImage    = "bitnamilegacy/minio:2025.7.23"
	// pollTimeout — предел ожидания результата задачи воркера.
	pollTimeout = 120 * time.Second
)

// Тест проверяет круговой сценарий воркера: задача ImportRequested → скачивание
// архива из MinIO → миграции и импорт в PostgreSQL → событие ObjectsChanged →
// метрики и проверки состояния на HTTP-эндпоинтах воркера.
func TestWorkerImportRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	databaseURL := startPostgres(t, ctx)
	natsURL := startNATS(t, ctx)
	s3endpoint, s3access, s3secret := startMinIO(t, ctx)

	// Архив: корневой справочник типов домов и региональный файл домов.
	bucket, key := "gar-archives", "test/gar_xml.zip"
	content := makeZIP(t, []archiveFile{
		{"AS_HOUSE_TYPES_20261019_1.xml", document(gar.KindHouseTypes,
			`<HOUSETYPE ID="1" NAME="Дом" SHORTNAME="д" DESC="дом" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTIVE="true"/>`)},
		{"77/AS_HOUSES_20261019_1.xml", document(gar.KindHouses, houseRecord(1)+houseRecord(2))},
	})
	putObject(t, ctx, s3endpoint, s3access, s3secret, bucket, key, content)

	workerErr := make(chan error, 1)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	metricsAddr := freeAddr(t)
	go func() {
		workerErr <- worker.Run(workerCtx, worker.Config{
			Config: config.Config{
				DatabaseURL:     databaseURL,
				MaxOpenConns:    4,
				MaxIdleConns:    4,
				ConnMaxIdleTime: time.Minute,
				ConnectTimeout:  10 * time.Second,
			},
			NATSUrl:         natsURL,
			S3:              objectstorage.Config{Endpoint: s3endpoint, AccessKey: s3access, SecretKey: s3secret},
			S3BucketDefault: bucket,
			MetricsAddr:     metricsAddr,
			ServiceName:     "importer-test",
			MigrationsDir:   "../../migrations/atlas",
		})
	}()
	t.Cleanup(func() {
		stopWorker()
		select {
		case err := <-workerErr:
			if err != nil {
				t.Errorf("worker.Run: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("worker.Run не завершился после отмены контекста")
		}
	})

	// Издатель и наблюдатель событий — отдельное соединение с шиной.
	conn, err := events.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.EnsureStream(ctx, js); err != nil {
		t.Fatal(err)
	}
	changes, err := events.NewSubscriber(js).SubscribeObjectsChanged(ctx, "test-changed")
	if err != nil {
		t.Fatal(err)
	}
	if err := events.NewPublisher(js).PublishImportRequested(ctx, events.ImportRequested{
		RequestID:   "req-worker-integration",
		Bucket:      bucket,
		Key:         key,
		Regions:     []int{77},
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	// Импорт записал дома региона 77 (миграции применяет сам воркер).
	database := openDB(t, ctx, databaseURL)
	deadline := time.Now().Add(pollTimeout)
	var houses int64
	for time.Now().Before(deadline) {
		select {
		case err := <-workerErr:
			t.Fatalf("worker.Run завершился до результата: %v", err)
		default:
		}
		if err := database.SQL().QueryRowContext(ctx, "SELECT count(*) FROM gar.houses").Scan(&houses); err == nil && houses > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if houses == 0 {
		t.Fatal("дома не импортированы за отведённое время")
	}

	// Событие о завершении файла домов опубликовано в шину.
	gotHouses := false
	for !gotHouses {
		select {
		case msg, ok := <-changes:
			if !ok {
				t.Fatal("канал ObjectsChanged закрыт до получения события")
			}
			if msg.Payload.Kind == string(gar.KindHouses) {
				if msg.Payload.Region != 77 || msg.Payload.Written != 2 || msg.Payload.RunID == 0 {
					t.Fatalf("искажённое событие: %+v", msg.Payload)
				}
				gotHouses = true
			}
			if err := msg.Ack(); err != nil {
				t.Fatal(err)
			}
		case <-time.After(pollTimeout):
			t.Fatal("событие ObjectsChanged(AS_HOUSES) не получено за отведённое время")
		}
	}

	// HTTP-эндпоинты воркера: живость, готовность и метрики импорта.
	assertHTTP(t, "http://"+metricsAddr+"/healthz", "ok")
	assertHTTP(t, "http://"+metricsAddr+"/readyz", "ok")
	metrics := assertHTTP(t, "http://"+metricsAddr+"/metrics", "gar_import_records_total")
	for _, want := range []string{`kind="AS_HOUSES"`, `gar_import_files_total`, `gar_worker_tasks_total`, `result="ack"`} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("в /metrics нет %q", want)
		}
	}
}

func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("gar"),
		tcpostgres.WithUsername("gar"),
		tcpostgres.WithPassword("gar"),
		// Двойное ожидание готовности: postgres перезапускает себя после
		// первого старта, а на macOS порт проксируется с задержкой.
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("docker с PostgreSQL недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера PostgreSQL: %v", err)
		}
	})
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return databaseURL
}

func startNATS(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tcnats.Run(ctx, natsImage)
	if err != nil {
		t.Skipf("docker с NATS недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера NATS: %v", err)
		}
	})
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func startMinIO(t *testing.T, ctx context.Context) (endpoint, accessKey, secretKey string) {
	t.Helper()
	// Образ bitnami запускается не от root: каталог /data недоступен для записи.
	container, err := tcminio.Run(ctx, minioImage, testcontainers.WithCmd("server", "/tmp/minio-data"))
	if err != nil {
		t.Skipf("docker с MinIO недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера MinIO: %v", err)
		}
	})
	endpoint, err = container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint, container.Username, container.Password
}

// openDB подключается к тестовой базе для чтения результата импорта.
func openDB(t *testing.T, ctx context.Context, databaseURL string) *storage.Store {
	t.Helper()
	db, err := storage.Open(ctx, config.Config{
		DatabaseURL:    databaseURL,
		MaxOpenConns:   2,
		MaxIdleConns:   2,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// putObject создаёт бакет (если нужно) и загружает архив в MinIO.
func putObject(t *testing.T, ctx context.Context, endpoint, accessKey, secretKey, bucket, key string, content []byte) {
	t.Helper()
	raw, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		if minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
			t.Fatal(err)
		}
	}
	if _, err := raw.PutObject(ctx, bucket, key, bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
}

type archiveFile struct{ name, content string }

// makeZIP собирает в памяти ZIP-архив выгрузки из файлов плана.
func makeZIP(t *testing.T, files []archiveFile) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
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
	return buffer.Bytes()
}

// document оборачивает записи в корневой элемент формата.
func document(kind gar.Kind, records string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><` + kind.RootElement() + `>` + records + `</` + kind.RootElement() + `>`
}

func houseRecord(id int) string {
	return fmt.Sprintf(`<HOUSE ID="%d" OBJECTID="%d" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" HOUSENUM="%d" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="1900-01-01" ENDDATE="2079-06-06" ISACTUAL="1" ISACTIVE="1"/>`, id, id, id)
}

// freeAddr возвращает свободный адрес 127.0.0.1:<port> для HTTP-сервера теста.
func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// assertHTTP запрашивает endpoint воркера и проверяет код 200 и маркер в теле.
func assertHTTP(t *testing.T, url, marker string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		response, err := http.Get(url) //nolint:bodyclose // тело читается и закрывается ниже
		if err == nil {
			body, rerr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if rerr == nil && response.StatusCode == http.StatusOK && strings.Contains(string(body), marker) {
				return string(body)
			}
			err = fmt.Errorf("статус %d, тело %q", response.StatusCode, body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", url, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
