package statement

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"finpi/internal/textutil"
)

// Bank identifica o banco emissor do extrato.
type Bank string

const (
	BankMercadoPago Bank = "mercadopago"
	BankNubank      Bank = "nubank"
	BankSicredi     Bank = "sicredi"
)

// Label devolve o nome do banco para exibição.
func (b Bank) Label() string {
	switch b {
	case BankNubank:
		return "Nubank"
	case BankSicredi:
		return "Sicredi"
	}
	return "Mercado Pago"
}

const maxCleanLen = 200

var (
	// CPF/CNPJ com ou sem pontuação, CPF mascarado (•••.123.456-••) e sequências de 11 ou 14 dígitos.
	docRe      = regexp.MustCompile(`\d{3}\.\d{3}\.\d{3}-\d{2}|\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}|[•*]{3}\.\d{3}\.\d{3}-[•*]{2}|\b\d{14}\b|\b\d{11}\b`)
	bankCodeRe = regexp.MustCompile(`\s*\(\d{4}\)`)
	agencyRe   = regexp.MustCompile(`(?i)\s*ag[eê]ncia:\s*\S+`)
	accountRe  = regexp.MustCompile(`(?i)\s*conta:\s*\S+`)
	// raiz de CNPJ solta, como "11.222.333 ", no começo do nome (Sicredi).
	cnpjRootRe = regexp.MustCompile(`^\d{1,3}(\.\d{3}){1,2}\s+`)
	// código do país no fim da compra no cartão, como " - BR".
	countrySuffixRe = regexp.MustCompile(`\s-\s[A-Za-z]{2}$`)
)

type kind struct{ prefix, label string }

// Os prefixos já vêm normalizados (textutil.Fold); os mais específicos vêm antes.
var nubankKinds = []kind{
	{"transferencia recebida pelo pix", "Pix recebido"},
	{"transferencia enviada pelo pix", "Pix enviado"},
	{"transferencia recebida", "Transferência recebida"},
	{"transferencia enviada", "Transferência enviada"},
	{"pagamento de boleto", "Pagamento de boleto"},
	{"pagamento de fatura", "Pagamento de fatura"},
	{"resgate rdb", "Resgate RDB"},
	{"aplicacao rdb", "Aplicação RDB"},
}

var sicrediKinds = []kind{
	{"recebimento pix", "Pix recebido"},
	{"pagamento pix", "Pix enviado"},
	{"pagamento boleto", "Pagamento de boleto"},
	{"debito automatico", "Débito automático"},
	{"integralizacao de capital", "Integralização de capital"},
	{"cartao debito", "Compra no débito"},
	{"recarga celular", "Recarga de celular"},
	{"transferencia conta salario", "Transferência conta salário"},
	{"devolucao pix", "Devolução Pix"},
}

func tidy(s string) string {
	s = strings.Trim(strings.Join(strings.Fields(s), " "), " -")
	if utf8.RuneCountInString(s) > maxCleanLen {
		s = strings.Trim(string([]rune(s)[:maxCleanLen]), " -")
	}
	return s
}

// capRunes corta em até maxCleanLen runas, sem quebrar caracteres multibyte.
func capRunes(s string) string {
	if r := []rune(s); len(r) > maxCleanLen {
		return string(r[:maxCleanLen])
	}
	return s
}

// stripNoise tira agência, conta, código de banco e documentos.
func stripNoise(s string) string {
	s = agencyRe.ReplaceAllString(s, "")
	s = accountRe.ReplaceAllString(s, "")
	s = bankCodeRe.ReplaceAllString(s, "")
	return docRe.ReplaceAllString(s, "")
}

func findKind(kinds []kind, head string) (kind, bool) {
	folded := textutil.Fold(head)
	for _, k := range kinds {
		if strings.HasPrefix(folded, k.prefix) {
			return k, true
		}
	}
	return kind{}, false
}

func cleanNubank(text string) string {
	parts := strings.Split(text, " - ")
	k, ok := findKind(nubankKinds, parts[0])
	if !ok {
		return ""
	}
	if len(parts) > 1 {
		if name := tidy(stripNoise(parts[1])); name != "" {
			return k.label + " " + name
		}
	}
	return k.label
}

func cleanSicredi(text string) string {
	parts := strings.Split(text, " - ")
	k, ok := findKind(sicrediKinds, parts[0])
	if !ok {
		return ""
	}
	if k.prefix == "integralizacao de capital" || len(parts) == 1 {
		return k.label
	}
	body := strings.Join(parts[1:], " - ")
	if k.prefix == "cartao debito" {
		body = countrySuffixRe.ReplaceAllString(body, "")
	}
	rest := tidy(stripNoise(body))
	rest = cnpjRootRe.ReplaceAllString(rest, "")
	if rest == "" {
		return k.label
	}
	return k.label + " " + rest
}

// CleanDescription padroniza a descrição de um lançamento: começa pelo tipo e tira CPF/CNPJ, banco, agência e
// conta. O Mercado Pago já vem limpo (só colapsa os espaços). Nunca devolve vazio: cai no original.
func CleanDescription(bank Bank, original string) string {
	text := strings.Join(strings.Fields(original), " ")
	var out string
	switch bank {
	case BankNubank:
		out = cleanNubank(text)
	case BankSicredi:
		out = cleanSicredi(text)
	default:
		if out = tidy(text); out == "" {
			out = capRunes(text)
		}
		return out
	}
	if out == "" {
		out = stripNoise(text)
	}
	if out = tidy(out); out == "" {
		out = tidy(text)
	}
	if out == "" {
		out = capRunes(text)
	}
	return out
}
