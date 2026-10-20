package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"gar-importer/internal/gar"
	"gar-importer/internal/garxml"
)

// TestFixtureValid проверяет, что каждая запись фикстуры проходит строгую
// валидацию по спецификациям XSD (internal/gar), а план архива не содержит
// неизвестных файлов.
func TestFixtureValid(t *testing.T) {
	files, err := buildFixture(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("фикстура пуста")
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.name)
	}
	plan := gar.Plan(names)
	if unknown := gar.Unknown(plan); len(unknown) > 0 {
		t.Fatalf("неизвестные файлы в плане: %+v", unknown)
	}
	for i, entry := range plan {
		if entry.Status != gar.StatusSelected {
			t.Fatalf("%s: статус %s (%s)", entry.Path, entry.Status, entry.Reason)
		}
		spec, ok := gar.Spec(entry.Kind)
		if !ok {
			t.Fatalf("%s: нет спецификации формата %s", entry.Path, entry.Kind)
		}
		var count int64
		consume := func(decoder *xml.Decoder, start xml.StartElement) error {
			if err := gar.ValidateRecord(entry.Kind, spec, start); err != nil {
				return err
			}
			count++
			var discard struct{}
			return decoder.DecodeElement(&discard, &start)
		}
		result, err := garxml.DecodeXML(context.Background(), bytes.NewReader([]byte(files[i].content)), entry.Kind, 0, consume)
		if err != nil {
			t.Fatalf("%s: %v", entry.Path, err)
		}
		if result.Records != int64(files[i].records) || count != int64(files[i].records) {
			t.Fatalf("%s: декодировано %d/%d записей, ожидалось %d", entry.Path, result.Records, count, files[i].records)
		}
	}
}

// TestFixtureDeterministic проверяет, что повторная генерация даёт побайтно
// тот же архив (воспроизводимость для CI).
func TestFixtureDeterministic(t *testing.T) {
	dir := t.TempDir()
	var digests [][]byte
	for i := 0; i < 2; i++ {
		files, err := buildFixture(1)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(dir, "fixture.zip")
		if err := writeZIP(name, files); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		digests = append(digests, data)
	}
	if !bytes.Equal(digests[0], digests[1]) {
		t.Fatal("повторная генерация дала другой архив")
	}
}

// TestFixtureRegion проверяет код региона в путях и границы параметра.
func TestFixtureRegion(t *testing.T) {
	files, err := buildFixture(50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		region, err := gar.RegionCode(file.name)
		if err != nil {
			t.Fatalf("%s: %v", file.name, err)
		}
		if filepath.Dir(file.name) != "." {
			if region != 50 {
				t.Fatalf("%s: регион %d, ожидался 50", file.name, region)
			}
			found = true
		} else if region != 0 {
			t.Fatalf("%s: справочник должен быть в корне", file.name)
		}
	}
	if !found {
		t.Fatal("нет файлов региона")
	}
	if _, err := buildFixture(0); err == nil {
		t.Fatal("регион 0 должен отвергаться")
	}
	if _, err := buildFixture(100); err == nil {
		t.Fatal("регион 100 должен отвергаться")
	}
}
