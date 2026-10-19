package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"gar-importer/internal/cli"
)

func TestCLIVersion(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if err := cli.Run(context.Background(), []string{"version"}, stdout, stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "gar-importer") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}} {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		err := cli.Run(context.Background(), args, stdout, stderr)
		if !errors.Is(err, cli.ErrUsage) {
			t.Fatalf("args %v: want ErrUsage, got %v", args, err)
		}
		if !strings.Contains(stderr.String(), "gar-importer") {
			t.Fatalf("args %v: no usage in stderr", args)
		}
	}
	// Ошибка флагов возвращает ErrUsage без общего текста помощи.
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if err := cli.Run(context.Background(), []string{"migrate", "extra"}, stdout, stderr); !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("migrate extra: want ErrUsage, got %v", err)
	}
}

func TestCLIImportFlagValidation(t *testing.T) {
	// Ошибки флагов выявляются до открытия БД и архива.
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"import"}, "usage"},
		{[]string{"import", "--zip", "x.zip", "--regions", "0"}, "неверный регион"},
		{[]string{"import", "--zip", "x.zip", "--regions", "100"}, "неверный регион"},
		{[]string{"import", "--zip", "x.zip", "--formats", "NO_SUCH_FORMAT"}, "неизвестный формат"},
		{[]string{"import", "--zip", "x.zip", "--batch-size", "5000"}, "размер пакета"},
	}
	for _, c := range cases {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		err := cli.Run(context.Background(), c.args, stdout, stderr)
		if err == nil {
			t.Fatalf("args %v: want error", c.args)
		}
		if c.want == "usage" {
			if !errors.Is(err, cli.ErrUsage) {
				t.Fatalf("args %v: want ErrUsage, got %v", c.args, err)
			}
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("args %v: want %q in %v", c.args, c.want, err)
		}
	}
}
