//go:build integration

package objectstorage_test

import (
	"archive/zip"
	"bytes"
	"context"
	"hash/crc32"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"gar-importer/internal/objectstorage"
)

// minioImage — пин образа для воспроизводимости теста (официальные образы
// MinIO убраны из Docker Hub, используется зеркало Bitnami Legacy).
const minioImage = "bitnamilegacy/minio:2025.7.23"

// Тест проверяет Stat, Exists и Download против MinIO в контейнере:
// содержимое скачанного файла совпадает с загруженным (crc32).
func TestDownloadRoundTrip(t *testing.T) {
	client, raw := startMinIO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	content := makeZip(t, "77/AS_HOUSES_20261019_TEST.XML", "<HOUSES></HOUSES>")
	bucket, key := "gar-archives", "2026-10/gar_xml.zip"
	putObject(t, ctx, raw, bucket, key, content)

	exists, err := client.Exists(ctx, bucket, key)
	if err != nil || !exists {
		t.Fatalf("Exists: %v, %v", exists, err)
	}
	exists, err = client.Exists(ctx, bucket, "нет/такого.zip")
	if err != nil || exists {
		t.Fatalf("Exists отсутствующего: %v, %v", exists, err)
	}
	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(content)) {
		t.Fatalf("размер: got %d, want %d", info.Size, len(content))
	}

	destDir := t.TempDir()
	downloadPath, size, err := client.Download(ctx, bucket, key, destDir)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(content)) {
		t.Fatalf("скачано байт: got %d, want %d", size, len(content))
	}
	downloaded, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if crc32.ChecksumIEEE(downloaded) != crc32.ChecksumIEEE(content) {
		t.Fatal("содержимое скачанного файла не совпадает (crc32)")
	}
}

// Тест проверяет OpenArchive: архив скачивается и открывается через zipgar,
// cleanup удаляет временный файл.
func TestOpenArchive(t *testing.T) {
	client, raw := startMinIO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	content := makeZip(t, "01/AS_ADDR_OBJ_20261019_TEST.XML", "<ADDRESSOBJECTS></ADDRESSOBJECTS>")
	putObject(t, ctx, raw, "gar-archives", "delta.zip", content)

	archive, cleanup, err := client.OpenArchive(ctx, "gar-archives", "delta.zip")
	if err != nil {
		t.Fatal(err)
	}
	names := archive.Names()
	if len(names) != 1 || names[0] != "01/AS_ADDR_OBJ_20261019_TEST.XML" {
		t.Fatalf("Names: %v", names)
	}
	reader, err := archive.Open(names[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "<ADDRESSOBJECTS></ADDRESSOBJECTS>" {
		t.Fatalf("содержимое XML: %q, %v", data, err)
	}
	path := archive.(interface{ Path() string }).Path()
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("временный файл не удалён: %v", err)
	}
}

func startMinIO(t *testing.T) (*objectstorage.Client, *minio.Client) {
	t.Helper()
	ctx := context.Background()
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
	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := objectstorage.Config{
		Endpoint:  endpoint,
		AccessKey: container.Username,
		SecretKey: container.Password,
	}
	client, err := objectstorage.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(container.Username, container.Password, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, raw
}

// putObject создаёт бакет (если нужно) и загружает объект.
func putObject(t *testing.T, ctx context.Context, raw *minio.Client, bucket, key string, content []byte) {
	t.Helper()
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

// makeZip собирает в памяти ZIP с одним XML-файлом.
func makeZip(t *testing.T, name, xml string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(xml)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
