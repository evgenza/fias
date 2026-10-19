// Package zipgar — источник данных: инвентаризация локального ZIP-архива ГАР
// и открытие его XML-файлов без распаковки на диск. Реализует порт
// importjob.Archive.
package zipgar

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Archive открывает ZIP-архив только для чтения; имена и содержимое
// доступны по каждому файлу.
type Archive struct {
	path    string
	file    *zip.ReadCloser
	names   []string
	entries map[string]*zip.File
}

// Open открывает локальный архив; поддержка удаления после чтения не требуется:
// читается по одному файлу.
func Open(archivePath string) (*Archive, error) {
	file, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("открытие ZIP: %w", err)
	}
	archive := &Archive{path: archivePath, file: file, entries: make(map[string]*zip.File)}
	for _, entry := range file.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		archive.names = append(archive.names, entry.Name)
		archive.entries[entry.Name] = entry
	}
	return archive, nil
}

// Names возвращает пути файлов архива (без каталогов) в порядке следования;
// отбор XML выполняет сценарий, чтобы отражать прочие файлы в отчёте.
func (a *Archive) Names() []string { return a.names }

// Open открывает XML-файл внутри архива.
func (a *Archive) Open(name string) (io.ReadCloser, error) {
	entry, ok := a.entries[name]
	if !ok {
		return nil, fmt.Errorf("zipgar: %q: файл отсутствует в архиве", name)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// Path возвращает путь архива (для журнала запусков).
func (a *Archive) Path() string { return a.path }

// Digest вычисляет sha256 содержимого архива потоковым чтением: идентичность
// источника не сводится к имени, дате или размеру. Кеш по mtime не считается
// доказательством неизменности при resume.
func (a *Archive) Digest() (string, error) {
	file, err := os.Open(a.path)
	if err != nil {
		return "", fmt.Errorf("zipgar: вычисление digest: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("zipgar: вычисление digest: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Close закрывает архив.
func (a *Archive) Close() error { return a.file.Close() }
