package statement

import (
	"strconv"
	"strings"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

// ExternalIDPrefix marca lançamentos importados do Mercado Pago.
const ExternalIDPrefix = "mp:"

const (
	externalIDPrefixNubank  = "nu:"
	externalIDPrefixSicredi = "sic:"
)

type Status string

const (
	StatusNew         Status = "novo"
	StatusImported    Status = "ja_importado"
	StatusTypeNew     Status = "tipo_novo"
	StatusPossibleDup Status = "possivel_duplicado"
)

// PreviewInput reúne o que vem do banco: identificadores já importados, chaves de lançamentos manuais,
// a última categoria por descrição normalizada (textutil.Fold) e os nomes de categoria disponíveis.
type PreviewInput struct {
	Bank           Bank // vazio = Mercado Pago
	ExistingIDs    map[string]bool
	ManualKeys     map[string]bool
	CategoryByDesc map[string]string
	Categories     []string
}

type PreviewRow struct {
	Index       int
	Date        string
	Description string
	Original    string // descrição como veio no arquivo
	Type        string // "income" ou "expense"
	AmountCents int64  // módulo
	SignedCents int64
	ExternalID  string
	Category    string
	Status      Status
	Selected    bool
	Locked      bool
}

type Preview struct {
	Bank            Bank
	Format          string // preenchido pelo handler
	Rows            []PreviewRow
	Credits, Debits int64 // somas das linhas lidas (débitos em módulo)
	FileCredits     int64 // resumo do arquivo
	FileDebits      int64 // resumo do arquivo, em módulo
	HasSummary      bool
	SummaryMismatch bool
	FirstDate       string
	LastDate        string
}

// ExternalID monta o identificador de origem: mp:<REFERENCE_ID>|<data>|<centavos com sinal>.
func ExternalID(ref, date string, cents int64) string {
	return ExternalIDPrefix + ref + "|" + date + "|" + strconv.FormatInt(cents, 10)
}

// ExternalIDFor monta o identificador de origem do lançamento: nu:<uuid>, sic:<identificador> ou, no Mercado Pago,
// mp:<ref>|<data>|<centavos>.
func ExternalIDFor(bank Bank, ref, date string, cents int64) string {
	switch bank {
	case BankNubank:
		return externalIDPrefixNubank + ref
	case BankSicredi:
		return externalIDPrefixSicredi + ref
	}
	return ExternalID(ref, date, cents)
}

// ValidExternalID diz se o identificador tem um dos prefixos aceitos.
func ValidExternalID(id string) bool {
	for _, p := range []string{ExternalIDPrefix, externalIDPrefixNubank, externalIDPrefixSicredi} {
		if strings.HasPrefix(id, p) && len(id) > len(p) {
			return true
		}
	}
	return false
}

var knownTypes = map[Bank][]string{
	BankMercadoPago: {"rendimentos", "pix recebido", "pix enviado"},
	BankNubank: {"pix recebido", "pix enviado", "transferencia recebida", "transferencia enviada",
		"pagamento de boleto", "pagamento de fatura", "resgate rdb", "aplicacao rdb"},
	BankSicredi: {"pix recebido", "pix enviado", "pagamento de boleto", "debito automatico", "integralizacao de capital",
		"compra no debito", "recarga de celular", "transferencia conta salario", "devolucao pix"},
}

func isKnownType(bank Bank, description string) bool {
	folded := textutil.Fold(description)
	for _, p := range knownTypes[bank] {
		if strings.HasPrefix(folded, p) {
			return true
		}
	}
	return false
}

// isInternalMovement: resgate e aplicação de RDB são movimentação entre contas do próprio dono.
func isInternalMovement(description string) bool {
	folded := textutil.Fold(description)
	return strings.HasPrefix(folded, "resgate rdb") || strings.HasPrefix(folded, "aplicacao rdb")
}

func defaultCategory(cats []string) string {
	for _, c := range cats {
		if textutil.Fold(c) == "outros" {
			return c
		}
	}
	if len(cats) > 0 {
		return cats[0]
	}
	return ""
}

// BuildPreview classifica cada linha (situação, seleção padrão, categoria sugerida) sem gravar nada.
func BuildPreview(st Statement, in PreviewInput) Preview {
	p := Preview{Rows: make([]PreviewRow, 0, len(st.Rows)), HasSummary: st.Summary.HasSummary}
	bank := in.Bank
	if bank == "" {
		bank = BankMercadoPago
	}
	p.Bank = bank
	fallback := defaultCategory(in.Categories)
	seen := map[string]int{}

	for i, r := range st.Rows {
		typ := "income"
		abs := r.AmountCents
		if r.AmountCents < 0 {
			typ = "expense"
			abs = -r.AmountCents
			p.Debits += abs
		} else {
			p.Credits += abs
		}

		id := ExternalIDFor(bank, r.RefID, r.Date, r.AmountCents)
		seen[id]++
		if n := seen[id]; n > 1 {
			id += "#" + strconv.Itoa(n)
		}

		category := fallback
		if c, ok := in.CategoryByDesc[textutil.Fold(r.Description)]; ok && c != "" {
			category = c
		}

		if bank != BankMercadoPago && isInternalMovement(r.Description) {
			category = models.InternalCategory
		}

		row := PreviewRow{
			Index: i, Date: r.Date, Description: r.Description, Original: r.Original, Type: typ,
			AmountCents: abs, SignedCents: r.AmountCents, ExternalID: id, Category: category,
		}
		switch {
		case in.ExistingIDs[id]:
			row.Status, row.Locked = StatusImported, true
		case !isKnownType(bank, r.Description):
			row.Status = StatusTypeNew
		case in.ManualKeys[models.TransactionKey(r.Date, abs, typ)]:
			row.Status = StatusPossibleDup
		default:
			row.Status, row.Selected = StatusNew, true
		}
		p.Rows = append(p.Rows, row)

		if p.FirstDate == "" || r.Date < p.FirstDate {
			p.FirstDate = r.Date
		}
		if r.Date > p.LastDate {
			p.LastDate = r.Date
		}
	}

	if st.Summary.HasSummary {
		p.FileCredits = st.Summary.Credits
		p.FileDebits = -st.Summary.Debits
		p.SummaryMismatch = p.Credits != p.FileCredits || p.Debits != p.FileDebits
	}
	return p
}
