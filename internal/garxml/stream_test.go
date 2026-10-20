package garxml

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"gar-importer/internal/gar"
)

func TestDecodeXML(t *testing.T) {
	var numbers []string
	consume := func(dec *xml.Decoder, start xml.StartElement) error {
		var record struct {
			Number string `xml:"HOUSENUM,attr"`
		}
		if err := dec.DecodeElement(&record, &start); err != nil {
			return err
		}
		numbers = append(numbers, record.Number)
		return nil
	}
	// Пустые атрибуты допускаются при декодировании: отдельного XSD-валидатора нет.
	result, err := DecodeXML(context.Background(), strings.NewReader("\ufeff<HOUSES><HOUSE HOUSENUM=\"93\"/><HOUSE/></HOUSES>"), gar.KindHouses, 0, consume)
	if err != nil || result.Records != 2 || result.Limited || len(numbers) != 2 || numbers[0] != "93" {
		t.Fatalf("%+v, %v, %v", result, numbers, err)
	}
	result, err = DecodeXML(context.Background(), strings.NewReader("<HOUSES><HOUSE/><HOUSE/><broken"), gar.KindHouses, 2, consume)
	if err != nil || result.Records != 2 || !result.Limited {
		t.Fatalf("sample: %+v, %v", result, err)
	}
	for _, src := range []string{"", "<HOUSES>", "<HOUSES><HOUSE/>", "<ROOMS/>", "<HOUSES><ROOM/></HOUSES>", "<HOUSES/></HOUSES>"} {
		if _, err := DecodeXML(context.Background(), strings.NewReader(src), gar.KindHouses, 0, consume); err == nil {
			t.Errorf("accepted broken XML %q", src)
		}
	}
}

func TestDecodeWindows1251(t *testing.T) {
	src, err := charmap.Windows1251.NewEncoder().String(`<?xml version="1.0" encoding="windows-1251"?><HOUSES><HOUSE HOUSENUM="Корпус 1"/></HOUSES>`)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	_, err = DecodeXML(context.Background(), strings.NewReader(src), gar.KindHouses, 0, func(dec *xml.Decoder, start xml.StartElement) error {
		var record struct {
			Name string `xml:"HOUSENUM,attr"`
		}
		err := dec.DecodeElement(&record, &start)
		name = record.Name
		return err
	})
	if err != nil || name != "Корпус 1" {
		t.Fatalf("name=%q, %v", name, err)
	}
}

func TestDecodeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := DecodeXML(ctx, strings.NewReader("<HOUSES><HOUSE/><HOUSE/></HOUSES>"), gar.KindHouses, 0, func(dec *xml.Decoder, start xml.StartElement) error {
		cancel()
		return dec.Skip()
	})
	if !errors.Is(err, context.Canceled) || result.Records != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
}

func TestDecodeStreamsBeforeEOF(t *testing.T) {
	reader, writer := io.Pipe()
	consumed := make(chan struct{})
	go func() {
		defer writer.Close()
		// Ошибки записи не ожидаются: читатель живёт до конца теста.
		_, _ = io.WriteString(writer, "<HOUSES><HOUSE/>")
		<-consumed
		_, _ = io.WriteString(writer, "</HOUSES>")
	}()
	defer reader.Close()
	result, err := DecodeXML(context.Background(), reader, gar.KindHouses, 0, func(dec *xml.Decoder, start xml.StartElement) error {
		if err := dec.Skip(); err != nil {
			return err
		}
		close(consumed)
		return nil
	})
	if err != nil || result.Records != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
}
