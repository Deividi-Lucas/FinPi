package statement

import (
	"bytes"
	"errors"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"finpi/internal/textutil"
)

// ErrUnknownBank indica um OFX de um banco que o FinPi ainda não lê.
var ErrUnknownBank = errors.New("o banco deste OFX ainda não é suportado: por enquanto o FinPi lê OFX do Nubank")

var (
	ofxOrgRe    = regexp.MustCompile(`(?i)<ORG>([^<\r\n]*)`)
	ofxSplitRe  = regexp.MustCompile(`(?i)<STMTTRN>`)
	ofxEndRe    = regexp.MustCompile(`(?i)</STMTTRN>|</BANKTRANLIST>`)
	ofxDateRe   = regexp.MustCompile(`(?i)<DTPOSTED>\s*(\d{8})`)
	ofxAmountRe = regexp.MustCompile(`(?i)<TRNAMT>([^<\r\n]*)`)
	ofxIDRe     = regexp.MustCompile(`(?i)<FITID>([^<\r\n]*)`)
	ofxMemoRe   = regexp.MustCompile(`(?i)<MEMO>([^<\r\n]*)`)
	ofxNameRe   = regexp.MustCompile(`(?i)<NAME>([^<\r\n]*)`)
)

// isOFX diz se o conteúdo é um arquivo OFX (pelo cabeçalho ou pela tag raiz, nos primeiros 2 KB).
func isOFX(data []byte) bool {
	head := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(head) > 2048 {
		head = head[:2048]
	}
	s := strings.ToUpper(string(head))
	return strings.Contains(s, "OFXHEADER") || strings.Contains(s, "<OFX>")
}

func ofxField(re *regexp.Regexp, block string) string {
	if m := re.FindStringSubmatch(block); m != nil {
		return html.UnescapeString(strings.TrimSpace(m[1]))
	}
	return ""
}

// parseOFX lê um OFX (SGML sem tags de fechamento ou XML). Só o Nubank é reconhecido, pelo <ORG>.
func parseOFX(data []byte) (Statement, error) {
	var text string
	if utf8.Valid(data) {
		text = string(data)
	} else {
		text = decodeWindows1252(data)
	}
	text = strings.TrimPrefix(text, "\ufeff")

	org := textutil.Fold(ofxField(ofxOrgRe, text))
	if !strings.Contains(org, "nu pagamentos") && !strings.Contains(org, "nubank") {
		return Statement{}, ErrUnknownBank
	}

	// Cada bloco vai de um <STMTTRN> até o próximo <STMTTRN> (o Split já o corta),
	// então os campos do bloco seguinte nunca entram no atual, mesmo sem tag de fechamento.
	blocks := ofxSplitRe.Split(text, -1)
	st := Statement{Bank: BankNubank, Format: "ofx"}
	for i, block := range blocks[1:] {
		if loc := ofxEndRe.FindStringIndex(block); loc != nil {
			block = block[:loc[0]]
		}
		line := i + 1

		m := ofxDateRe.FindStringSubmatch(block)
		if m == nil {
			return Statement{}, &RowError{line, "data inválida"}
		}
		date, err := time.Parse("20060102", m[1])
		if err != nil {
			return Statement{}, &RowError{line, "data inválida"}
		}
		original := ofxField(ofxMemoRe, block)
		if original == "" {
			original = ofxField(ofxNameRe, block)
		}
		original = strings.Join(strings.Fields(original), " ")
		if original == "" {
			return Statement{}, &RowError{line, "descrição vazia"}
		}
		ref := ofxField(ofxIDRe, block)
		if ref == "" {
			return Statement{}, &RowError{line, "sem identificador"}
		}
		if utf8.RuneCountInString(ref) > maxRefLen {
			return Statement{}, &RowError{line, "identificador longo demais"}
		}
		cents, err := parseCents(cell{text: ofxField(ofxAmountRe, block)})
		if err != nil {
			return Statement{}, &RowError{line, "valor inválido"}
		}
		if len(st.Rows) >= MaxRows {
			return Statement{}, ErrTooManyRows
		}
		st.Rows = append(st.Rows, Row{
			Line: line, Date: date.Format("2006-01-02"), Description: CleanDescription(BankNubank, original),
			Original: truncateRunes(original, maxOriginalRunes), AmountCents: cents, RefID: ref,
		})
	}
	if len(st.Rows) == 0 {
		return Statement{}, ErrEmpty
	}
	return st, nil
}
