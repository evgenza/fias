package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"gar-importer/internal/importjob"
	"gar-importer/internal/input/zipgar"
)

// Client — клиент S3/MinIO для архивов ГАР.
type Client struct {
	minio *minio.Client
}

// NewClient создаёт клиент по настройкам подключения.
func NewClient(config Config) (*Client, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("objectstorage: задайте endpoint (S3_ENDPOINT)")
	}
	raw, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("objectstorage: клиент %q: %w", config.Endpoint, err)
	}
	return &Client{minio: raw}, nil
}

// Download скачивает объект во временный файл в destDir (пусто — новый каталог
// os.MkdirTemp) и возвращает путь и размер. Запись атомарна: объект пишется
// во временное имя, синхронизируется (fsync) и переименовывается в файл
// с именем ключа объекта.
func (c *Client) Download(ctx context.Context, bucket, key, destDir string) (string, int64, error) {
	dir := destDir
	if dir == "" {
		created, err := os.MkdirTemp("", "gar-object-")
		if err != nil {
			return "", 0, fmt.Errorf("objectstorage: временный каталог: %w", err)
		}
		dir = created
	}
	name := path.Base(key)
	if name == "." || name == "/" || name == "" {
		return "", 0, fmt.Errorf("objectstorage: некорректный ключ объекта %q", key)
	}
	tmp, err := os.CreateTemp(dir, ".gar-download-*")
	if err != nil {
		return "", 0, fmt.Errorf("objectstorage: временный файл: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("objectstorage: временный файл: %w", err)
	}
	if err := c.minio.FGetObject(ctx, bucket, key, tmpPath, minio.GetObjectOptions{}); err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("objectstorage: скачивание s3://%s/%s: %w", bucket, key, err)
	}
	// fsync до rename: переименование не должно опережать запись на диск.
	file, err := os.OpenFile(tmpPath, os.O_RDONLY, 0)
	if err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("objectstorage: fsync s3://%s/%s: %w", bucket, key, err)
	}
	info, err := file.Stat()
	if err == nil {
		err = file.Sync()
	}
	cerr := file.Close()
	if err != nil || cerr != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("objectstorage: fsync s3://%s/%s: %w", bucket, key, errors.Join(err, cerr))
	}
	dest := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, dest); err != nil {
		os.Remove(tmpPath)
		return "", 0, fmt.Errorf("objectstorage: публикация файла %s: %w", dest, err)
	}
	return dest, info.Size(), nil
}

// Stat возвращает сведения об объекте.
func (c *Client) Stat(ctx context.Context, bucket, key string) (minio.ObjectInfo, error) {
	info, err := c.minio.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return minio.ObjectInfo{}, fmt.Errorf("objectstorage: stat s3://%s/%s: %w", bucket, key, err)
	}
	return info, nil
}

// Exists проверяет наличие объекта.
func (c *Client) Exists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.minio.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return false, nil
	}
	return false, fmt.Errorf("objectstorage: stat s3://%s/%s: %w", bucket, key, err)
}

// OpenArchive скачивает архив ГАР из хранилища и открывает его как
// importjob.Archive; cleanup закрывает архив и удаляет временный файл.
func (c *Client) OpenArchive(ctx context.Context, bucket, key string) (importjob.Archive, func() error, error) {
	archivePath, _, err := c.Download(ctx, bucket, key, "")
	if err != nil {
		return nil, nil, err
	}
	archive, err := zipgar.Open(archivePath)
	if err != nil {
		os.Remove(archivePath)
		return nil, nil, err
	}
	cleanup := func() error {
		return errors.Join(archive.Close(), os.Remove(archivePath))
	}
	return archive, cleanup, nil
}
