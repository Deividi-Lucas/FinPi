// Package statement lê extratos do Mercado Pago (XLSX ou CSV) e monta a revisão de importação.
// São funções puras: não tocam o banco nem o disco.
package statement

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	MaxFileBytes = 5 << 20 // tamanho máximo do arquivo
	MaxRows      = 5000    // máximo de lançamentos por extrato
	maxCents     = 10_000_000_000_000
)

var (
	ErrUnsupportedFormat = errors.New("formato de arquivo não suportado: envie um extrato .xlsx, .csv ou .ofx")
	ErrInvalidFile       = errors.New("não foi possível ler o arquivo: ele parece corrompido")
	ErrUnrecognized      = errors.New("formato não reconhecido: envie um extrato do Mercado Pago (XLSX ou CSV), do Nubank (OFX ou CSV) do Sicredi (planilha) ou do Banco do Brasil (CSV)")
	ErrScientificID      = errors.New("o identificador (REFERENCE_ID) está em notação científica: o arquivo foi aberto e salvo no Excel. Baixe o extrato de novo e envie o arquivo original, sem abrir no Excel")
	ErrTooLarge          = errors.New("o arquivo é grande demais (máximo de 5 MB)")
	ErrTooManyRows       = fmt.Errorf("o extrato tem mais de %d lançamentos", MaxRows)
	ErrEmpty             = errors.New("o arquivo não tem lançamentos")
)

// RowError indica um problema numa linha do arquivo (Line é o número da linha na planilha/arquivo).
type RowError struct {
	Line int
	Msg  string
}

func (e *RowError) Error() string { return fmt.Sprintf("linha %d: %s", e.Line, e.Msg) }

// Row é um lançamento do extrato. AmountCents tem sinal (positivo entra, negativo sai).
type Row struct {
	Line        int
	Date        string // AAAA-MM-DD
	Description string
	AmountCents int64
	RefID       string
	Original    string // descrição como veio do banco (vazia no Mercado Pago)
}

// Summary é o resumo do topo do arquivo; Debits é negativo, como no arquivo.
type Summary struct {
	HasSummary bool
	Credits    int64
	Debits     int64
}

type Statement struct {
	Rows    []Row
	Summary Summary
	Bank    Bank
	Format  string // "xlsx", "csv" ou "ofx"
}

// cell é uma célula já lida; numeric indica célula numérica do XLSX (valor cru, com ponto decimal).
type cell struct {
	text    string
	numeric bool
}

// Parse lê o extrato (XLSX ou CSV) e devolve os lançamentos. Não grava nada.
func Parse(filename string, data []byte) (Statement, error) {
	if len(data) > MaxFileBytes {
		return Statement{}, ErrTooLarge
	}
	var (
		grid   [][]cell
		err    error
		format string
	)
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		// A assinatura ZIP vem antes da detecção de OFX: uma planilha nunca vai para o leitor de OFX.
		grid, err = readXLSX(data)
		format = "xlsx"
	case isOFX(data):
		return parseOFX(data)
	case strings.HasSuffix(strings.ToLower(filename), ".csv"):
		grid, err = readCSV(data)
		format = "csv"
	default:
		return Statement{}, ErrUnsupportedFormat
	}
	if err != nil {
		return Statement{}, err
	}
	st, err := parseGrid(grid)
	if err != nil {
		return Statement{}, err
	}
	st.Format = format
	return st, nil
}

var requiredColumns = []string{"RELEASE_DATE", "TRANSACTION_TYPE", "REFERENCE_ID", "TRANSACTION_NET_AMOUNT"}

func normHeader(s string) string {
	return strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(s, string(rune(0xFEFF)))))
}

func rowEmpty(row []cell) bool {
	for _, c := range row {
		if strings.TrimSpace(c.text) != "" {
			return false
		}
	}
	return true
}

// parseMercadoPago lê o extrato do Mercado Pago (cabeçalho RELEASE_DATE).
func parseMercadoPago(grid [][]cell) (Statement, error) {
	headerRow := -1
	for i, row := range grid {
		for _, c := range row {
			if normHeader(c.text) == "RELEASE_DATE" {
				headerRow = i
				break
			}
		}
		if headerRow >= 0 {
			break
		}
	}
	if headerRow < 0 {
		return Statement{}, ErrUnrecognized
	}

	col := map[string]int{}
	for j, c := range grid[headerRow] {
		if h := normHeader(c.text); h != "" {
			if _, ok := col[h]; !ok {
				col[h] = j
			}
		}
	}
	for _, name := range requiredColumns {
		if _, ok := col[name]; !ok {
			return Statement{}, ErrUnrecognized
		}
	}

	st := Statement{Summary: readSummary(grid[:headerRow]), Bank: BankMercadoPago}
	for i := headerRow + 1; i < len(grid); i++ {
		row := grid[i]
		if rowEmpty(row) {
			continue
		}
		line := i + 1
		get := func(name string) cell {
			if j := col[name]; j < len(row) {
				return row[j]
			}
			return cell{}
		}

		date, err := parseDate(get("RELEASE_DATE"))
		if err != nil {
			return Statement{}, &RowError{line, "data inválida"}
		}
		desc := strings.Join(strings.Fields(get("TRANSACTION_TYPE").text), " ")
		if desc == "" {
			return Statement{}, &RowError{line, "descrição vazia"}
		}
		ref, err := parseRef(get("REFERENCE_ID"))
		if err != nil {
			if errors.Is(err, ErrScientificID) {
				return Statement{}, err
			}
			return Statement{}, &RowError{line, err.Error()}
		}
		cents, err := parseCents(get("TRANSACTION_NET_AMOUNT"))
		if err != nil {
			return Statement{}, &RowError{line, "valor inválido"}
		}

		if len(st.Rows) >= MaxRows {
			return Statement{}, ErrTooManyRows
		}
		st.Rows = append(st.Rows, Row{Line: line, Date: date, Description: desc, AmountCents: cents, RefID: ref})
	}
	if len(st.Rows) == 0 {
		return Statement{}, ErrEmpty
	}
	return st, nil
}

// readSummary lê o bloco do topo (linha com CREDITS e DEBITS e a linha de valores logo abaixo).
func readSummary(rows [][]cell) Summary {
	for i, row := range rows {
		credits, debits := -1, -1
		for j, c := range row {
			switch normHeader(c.text) {
			case "CREDITS":
				credits = j
			case "DEBITS":
				debits = j
			}
		}
		if credits < 0 || debits < 0 || i+1 >= len(rows) {
			continue
		}
		vals := rows[i+1]
		if credits >= len(vals) || debits >= len(vals) {
			return Summary{}
		}
		cr, err1 := parseMoney(vals[credits])
		db, err2 := parseMoney(vals[debits])
		if err1 != nil || err2 != nil {
			return Summary{}
		}
		return Summary{HasSummary: true, Credits: cr, Debits: db}
	}
	return Summary{}
}

func parseDate(c cell) (string, error) {
	s := strings.TrimSpace(c.text)
	if c.numeric {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return "", errors.New("data inválida")
		}
		days := int(math.Floor(f))
		if days < 20000 || days > 80000 {
			return "", errors.New("data inválida")
		}
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days).Format("2006-01-02"), nil
	}
	for _, layout := range []string{"02-01-2006", "02/01/2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", errors.New("data inválida")
}

var sciRe = regexp.MustCompile(`(?i)^\d+([.,]\d+)?e[+-]?\d{1,3}$`)

// parseRef devolve o REFERENCE_ID. Notação científica em texto significa que o Excel destruiu o número.
func parseRef(c cell) (string, error) {
	s := strings.TrimSpace(c.text)
	if s == "" {
		return "", errors.New("REFERENCE_ID vazio")
	}
	if sciRe.MatchString(s) {
		if !c.numeric {
			return "", ErrScientificID
		}
		r, ok := new(big.Rat).SetString(strings.Replace(s, ",", ".", 1))
		if !ok || !r.IsInt() {
			return "", ErrScientificID
		}
		return r.Num().String(), nil
	}
	return s, nil
}

var (
	thousandsRe = regexp.MustCompile(`^[+-]?\d{1,3}(\.\d{3})+$`)
	decimalRe   = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d{1,2})?$`)
)

// brToDecimal converte texto brasileiro ("1.234,56") em decimal com ponto ("1234.56").
func brToDecimal(s string) string {
	if strings.Contains(s, ",") {
		return strings.Replace(strings.ReplaceAll(s, ".", ""), ",", ".", 1)
	}
	if thousandsRe.MatchString(s) {
		return strings.ReplaceAll(s, ".", "")
	}
	return s
}

// parseMoney converte a célula em centavos sem ponto flutuante. Texto aceita no máximo 2 casas;
// célula numérica do XLSX é arredondada (meio para longe do zero).
func parseMoney(c cell) (int64, error) {
	s := strings.NewReplacer(" ", "", " ", "").Replace(strings.TrimSpace(c.text))
	s = strings.TrimPrefix(s, "R$")
	if s == "" {
		return 0, errors.New("valor vazio")
	}
	if !c.numeric {
		s = brToDecimal(s)
		if strings.ContainsAny(s, "eE") {
			return 0, errors.New("valor inválido")
		}
	}
	if !decimalRe.MatchString(s) {
		return 0, errors.New("valor inválido")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, errors.New("valor inválido")
	}
	r.Mul(r, big.NewRat(100, 1))
	if !c.numeric && !r.IsInt() {
		return 0, errors.New("valor inválido")
	}

	num := new(big.Int).Abs(r.Num())
	den := r.Denom()
	num.Mul(num, big.NewInt(2))
	num.Add(num, den)
	num.Div(num, new(big.Int).Mul(den, big.NewInt(2)))
	if r.Sign() < 0 {
		num.Neg(num)
	}
	if !num.IsInt64() {
		return 0, errors.New("valor inválido")
	}
	return num.Int64(), nil
}

// parseCents é parseMoney para lançamentos: valor zero ou acima do limite é inválido.
func parseCents(c cell) (int64, error) {
	v, err := parseMoney(c)
	if err != nil || v == 0 || v > maxCents || v < -maxCents {
		return 0, errors.New("valor inválido")
	}
	return v, nil
}
