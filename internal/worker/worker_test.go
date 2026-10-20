package worker

import (
	"archive/zip"
	"compress/flate"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minio/minio-go/v7"

	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
)

// Детерминированные ошибки данных и архива — постоянные: повторная доставка
// задачи с тем же архивом их не исправит. Временные (сеть, БД, отмена) — Nak.
func TestIsPermanent(t *testing.T) {
	permanent := []struct {
		name string
		err  error
	}{
		{"bad request", fmt.Errorf("обёртка: %w", errBadRequest)},
		{"record error", &gar.RecordError{Kind: gar.KindHouses, Attribute: "OPERTYPEID", Cause: "не помещается в int64"}},
		{"record error wrapped", fmt.Errorf("AS_HOUSES.xml: %w", &gar.RecordError{Kind: gar.KindHouses, Cause: "корень <FOO>"})},
		{"xml syntax", fmt.Errorf("XML, байт 10: %w", &xml.SyntaxError{Msg: "неожиданный EOF"})},
		{"zip format", zip.ErrFormat},
		{"zip checksum", zip.ErrChecksum},
		{"zip truncated", fmt.Errorf("read: %w", io.ErrUnexpectedEOF)},
		{"flate corrupt", fmt.Errorf("inflate: %w", flate.CorruptInputError(7))},
		{"no selected files", fmt.Errorf("архив.zip: %w", importjob.ErrNoSelectedFiles)},
		{"sqlstate invalid text", &pgconn.PgError{Code: "22P02", Message: "invalid input syntax for type uuid"}},
		{"sqlstate numeric overflow", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "22003"})},
		{"sqlstate unique violation", &pgconn.PgError{Code: "23505"}},
		{"sqlstate foreign key", &pgconn.PgError{Code: "23503"}},
		{"s3 no such key", minio.ErrorResponse{Code: "NoSuchKey"}},
		{"s3 no such bucket", minio.ErrorResponse{Code: "NoSuchBucket"}},
	}
	for _, test := range permanent {
		t.Run("permanent/"+test.name, func(t *testing.T) {
			if !isPermanent(test.err) {
				t.Fatalf("isPermanent(%v) = false, want true", test.err)
			}
		})
	}
	temporary := []struct {
		name string
		err  error
	}{
		{"generic", errors.New("временный сбой")},
		{"context canceled", context.Canceled},
		{"context deadline", context.DeadlineExceeded},
		{"sqlstate serialization", &pgconn.PgError{Code: "40001"}},
		{"sqlstate connection", &pgconn.PgError{Code: "08006"}},
		{"s3 access denied", minio.ErrorResponse{Code: "AccessDenied"}},
	}
	for _, test := range temporary {
		t.Run("temporary/"+test.name, func(t *testing.T) {
			if isPermanent(test.err) {
				t.Fatalf("isPermanent(%v) = true, want false", test.err)
			}
		})
	}
}
