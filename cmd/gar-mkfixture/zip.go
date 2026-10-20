package main

import (
	"archive/zip"
	"fmt"
	"os"
	"time"
)

// zipTime — фиксированное время модификации файлов архива: без него
// повторная генерация давала бы другой байтовый образ ZIP.
var zipTime = time.Date(2025, 10, 19, 0, 0, 0, 0, time.UTC)

// writeZIP записывает файлы фикстуры в ZIP-архив (Deflate) детерминированно:
// фиксированный порядок файлов и время модификации.
func writeZIP(path string, files []fixtureFile) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("создание архива %s: %w", path, err)
	}
	writer := zip.NewWriter(out)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.Modified = zipTime
		entry, err := writer.CreateHeader(header)
		if err != nil {
			out.Close()
			return fmt.Errorf("архив %s, файл %s: %w", path, file.name, err)
		}
		if _, err := entry.Write([]byte(file.content)); err != nil {
			out.Close()
			return fmt.Errorf("архив %s, файл %s: %w", path, file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		out.Close()
		return fmt.Errorf("закрытие архива %s: %w", path, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("закрытие архива %s: %w", path, err)
	}
	return nil
}
