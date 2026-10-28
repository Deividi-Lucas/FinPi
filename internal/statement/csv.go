package statement

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

var cp1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰',
	0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•',
	0x96: '–', 0x97: '—', 0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
}

// decodeWindows1252 converte bytes Windows-1252 em texto UTF-8.
func decodeWindows1252(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x80, c >= 0xA0:
			sb.WriteRune(rune(c))
		default:
			if r, ok := cp1252[c]; ok {
				sb.WriteRune(r)
			} else {
				sb.WriteRune('�')
			}
		}
	}
	return sb.String()
}

// detectDelimiter olha a linha do cabeçalho: tabulação, depois ponto e vírgula, depois vírgula.
func detectDelimiter(text string) rune {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(strings.ToUpper(line), "RELEASE_DATE") {
			continue
		}
		tabs, semis, commas := strings.Count(line, "\t"), strings.Count(line, ";"), strings.Count(line, ",")
		switch {
		case tabs >= semis && tabs >= commas && tabs > 0:
			return '\t'
		case semis >= commas && semis > 0:
			return ';'
		}
		break
	}
	return ','
}

func readCSV(data []byte) ([][]cell, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := string(data)
	if !utf8.Valid(data) {
		text = decodeWindows1252(data)
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = detectDelimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	var grid [][]cell
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrInvalidFile
		}
		row := make([]cell, len(rec))
		for i, v := range rec {
			row[i] = cell{text: v}
		}
		grid = append(grid, row)
		if len(grid) > MaxRows+100 {
			return nil, ErrTooManyRows
		}
	}
	return grid, nil
}
