// Package garxml — потоковое декодирование XML-файлов выгрузки ГАР.
// Сведения о форматах (Kind, элементы, спецификации записей) находятся
// в пакете internal/gar.
package garxml

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"gar-importer/internal/gar"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

type DecodeResult struct {
	Records int64
	Limited bool
}

// DecodeXML читает один документ по записи без загрузки файла в память.
// limit=0 означает весь файл. consume должен прочитать элемент через DecodeElement.
func DecodeXML(ctx context.Context, reader io.Reader, kind gar.Kind, limit int64, consume func(*xml.Decoder, xml.StartElement) error) (DecodeResult, error) {
	var result DecodeResult
	if !kind.Known() {
		return result, fmt.Errorf("неизвестный формат ГАР %q", kind)
	}
	if limit < 0 {
		return result, fmt.Errorf("лимит записей не может быть отрицательным")
	}
	decoder := xml.NewDecoder(reader)
	decoder.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "windows-1251", "cp1251":
			return transform.NewReader(r, charmap.Windows1251.NewDecoder()), nil
		case "utf-8", "utf8", "us-ascii":
			return r, nil
		default:
			return nil, fmt.Errorf("неподдерживаемая кодировка XML %q", label)
		}
	}
	opened, closed := false, false
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if !opened || !closed {
				return result, fmt.Errorf("XML-документ %s неполон: %w", kind, io.ErrUnexpectedEOF)
			}
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("XML, байт %d: %w", decoder.InputOffset(), err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			if !opened {
				if element.Name.Local != kind.RootElement() {
					return result, fmt.Errorf("корень <%s>, ожидается <%s> для %s", element.Name.Local, kind.RootElement(), kind)
				}
				opened = true
				continue
			}
			if closed || element.Name.Local != kind.RecordElement() {
				return result, fmt.Errorf("неожиданный элемент <%s> в %s на байте XML %d", element.Name.Local, kind, decoder.InputOffset())
			}
			if err := consume(decoder, element); err != nil {
				return result, fmt.Errorf("запись %d на байте XML %d: %w", result.Records+1, decoder.InputOffset(), err)
			}
			result.Records++
			if limit > 0 && result.Records >= limit {
				result.Limited = true
				return result, nil
			}
		case xml.EndElement:
			closed = true
		}
	}
}
