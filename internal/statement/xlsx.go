package statement

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"
)

const maxXMLBytes = 50 << 20 // proteção contra zip bomb: tamanho máximo de cada XML descompactado

type richText struct {
	T string `xml:"t"`
	R []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (r richText) text() string {
	var b strings.Builder
	b.WriteString(r.T)
	for _, x := range r.R {
		b.WriteString(x.T)
	}
	return b.String()
}

type sharedStrings struct {
	SI []richText `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		R int `xml:"r,attr"`
		C []struct {
			R  string   `xml:"r,attr"`
			T  string   `xml:"t,attr"`
			V  string   `xml:"v"`
			IS richText `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func readZipXML(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return ErrInvalidFile
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, maxXMLBytes+1))
	if err != nil || len(body) > maxXMLBytes {
		return ErrInvalidFile
	}
	if err := xml.Unmarshal(body, v); err != nil {
		return ErrInvalidFile
	}
	return nil
}

// colIndex converte a letra da coluna de uma referência ("D12" -> 3).
func colIndex(ref string) int {
	n := 0
	for _, c := range ref {
		if c < 'A' || c > 'Z' {
			break
		}
		n = n*26 + int(c-'A') + 1
	}
	return n - 1
}

// readXLSX lê a primeira planilha (sheet1.xml, ou a primeira por nome) como uma grade de células.
func readXLSX(data []byte) ([][]cell, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrInvalidFile
	}
	files := map[string]*zip.File{}
	var sheets []string
	for _, f := range zr.File {
		files[f.Name] = f
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheets = append(sheets, f.Name)
		}
	}
	if len(sheets) == 0 {
		return nil, ErrInvalidFile
	}
	sort.Strings(sheets)
	sheetName := sheets[0]
	if _, ok := files["xl/worksheets/sheet1.xml"]; ok {
		sheetName = "xl/worksheets/sheet1.xml"
	}

	var strs []string
	if f, ok := files["xl/sharedStrings.xml"]; ok {
		var sst sharedStrings
		if err := readZipXML(f, &sst); err != nil {
			return nil, err
		}
		for _, si := range sst.SI {
			strs = append(strs, si.text())
		}
	}

	var sheet xlsxSheet
	if err := readZipXML(files[sheetName], &sheet); err != nil {
		return nil, err
	}

	var grid [][]cell
	seq := 0
	for _, row := range sheet.Rows {
		seq++
		rn := row.R
		if rn <= 0 {
			rn = seq
		}
		seq = rn
		if rn > MaxRows+100 {
			return nil, ErrTooManyRows
		}
		for len(grid) < rn {
			grid = append(grid, nil)
		}
		cells := make([]cell, 0, len(row.C))
		for k, c := range row.C {
			idx := colIndex(c.R)
			if idx < 0 {
				idx = k
			}
			for len(cells) <= idx {
				cells = append(cells, cell{})
			}
			switch c.T {
			case "s":
				if i, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && i >= 0 && i < len(strs) {
					cells[idx] = cell{text: strs[i]}
				}
			case "inlineStr":
				cells[idx] = cell{text: c.IS.text()}
			case "str":
				cells[idx] = cell{text: c.V}
			case "b", "e":
				// booleano e erro: ignorados
			default:
				cells[idx] = cell{text: c.V, numeric: true}
			}
		}
		grid[rn-1] = cells
	}
	return grid, nil
}
