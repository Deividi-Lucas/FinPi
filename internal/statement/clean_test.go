package statement

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanDescription(t *testing.T) {
	cases := []struct {
		name string
		bank Bank
		in   string
		want string
	}{
		{"nubank pix recebido", BankNubank,
			"Transferência recebida pelo Pix - MARIA DA SILVA - •••.123.456-•• - BANCO EXEMPLO S.A. (0999) Agência: 1 Conta: 2-3",
			"Pix recebido MARIA DA SILVA"},
		{"nubank pix enviado", BankNubank,
			"Transferência enviada pelo Pix - Maria da Silva - •••.123.456-•• - BANCO TESTE S.A. (0999) Agência: 1 Conta: 1234567-8",
			"Pix enviado Maria da Silva"},
		{"nubank transferência recebida (TED)", BankNubank,
			"Transferência Recebida - EMPRESA EXEMPLO LTDA - 12.345.678/0001-90 - BCO EXEMPLO S.A. (0999) Agência: 123 Conta: 123-4",
			"Transferência recebida EMPRESA EXEMPLO LTDA"},
		{"nubank boleto", BankNubank, "Pagamento de boleto efetuado - DAS-SIMPLES NACIONAL", "Pagamento de boleto DAS-SIMPLES NACIONAL"},
		{"nubank fatura", BankNubank, "Pagamento de fatura", "Pagamento de fatura"},
		{"nubank resgate", BankNubank, "Resgate RDB", "Resgate RDB"},
		{"nubank aplicação", BankNubank, "Aplicação RDB", "Aplicação RDB"},
		{"nubank desconhecido", BankNubank, "Estorno de tarifa 12.345.678/0001-90", "Estorno de tarifa"},
		{"sicredi pix recebido", BankSicredi, "RECEBIMENTO PIX - 12345678901 MARIA DA SILVA", "Pix recebido MARIA DA SILVA"},
		{"sicredi pix enviado com raiz de CNPJ", BankSicredi,
			"PAGAMENTO PIX - 11222333000144 11.222.333 JOAO PEREIRA ME", "Pix enviado JOAO PEREIRA ME"},
		{"sicredi boleto", BankSicredi,
			"PAGAMENTO BOLETO -  12345678000100 SEGURADORA EXEMPLO S A - SERVICOS DIVERSOS",
			"Pagamento de boleto SEGURADORA EXEMPLO S A - SERVICOS DIVERSOS"},
		{"sicredi débito automático", BankSicredi, "DEBITO AUTOMATICO - 98765432000199 SEGURO EXEMPLO", "Débito automático SEGURO EXEMPLO"},
		{"sicredi integralização", BankSicredi, "INTEGRALIZACAO DE CAPITAL POR DEB CONTA", "Integralização de capital"},
		{"sicredi desconhecido", BankSicredi, "TARIFA PACOTE SERVICOS 12345678901 REF", "TARIFA PACOTE SERVICOS REF"},
		{"sicredi cartão débito", BankSicredi, "CARTAO DEBITO - MERCADO EXEMPLO CENTRO - BR", "Compra no débito MERCADO EXEMPLO CENTRO"},
		{"sicredi cartão débito com raiz de CNPJ", BankSicredi, "CARTAO DEBITO - 11.222.333 LOJA EXEMPLO - BR", "Compra no débito LOJA EXEMPLO"},
		{"sicredi cartão débito com asterisco", BankSicredi, "CARTAO DEBITO - SPG*GRILL EXEMPLO - BR", "Compra no débito SPG*GRILL EXEMPLO"},
		{"sicredi recarga", BankSicredi, "RECARGA CELULAR", "Recarga de celular"},
		{"sicredi conta salário", BankSicredi, "TRANSFERÊNCIA CONTA SALÁRIO - 12345678901 MARIA DA SILVA", "Transferência conta salário MARIA DA SILVA"},
		{"sicredi devolução pix", BankSicredi, "DEVOLUCAO PIX - 12345678901 MARIA DA SILVA", "Devolução Pix MARIA DA SILVA"},
		{"sicredi boleto mantém sufixo de duas letras", BankSicredi, "PAGAMENTO BOLETO - 12345678000100 EMPRESA EXEMPLO - SP", "Pagamento de boleto EMPRESA EXEMPLO - SP"},
		{"mercado pago não muda", BankMercadoPago, "Pix recebido  Fulano", "Pix recebido Fulano"},
	}
	for _, c := range cases {
		if got := CleanDescription(c.bank, c.in); got != c.want {
			t.Errorf("%s:\n  entrada: %q\n  obtido:  %q\n  quer:    %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCleanDescription_LimitsLengthAndNeverReturnsEmpty(t *testing.T) {
	long := "Pagamento de boleto efetuado - " + strings.Repeat("ABCDEFGHIJ", 40)
	got := CleanDescription(BankNubank, long)
	if n := utf8.RuneCountInString(got); n < 1 || n > 200 {
		t.Errorf("tamanho %d fora de 1..200", n)
	}
	if got := CleanDescription(BankSicredi, "12345678901"); got == "" {
		t.Error("descrição só de documento não pode virar vazia (usa o original)")
	}
}

func TestBankLabel(t *testing.T) {
	for b, want := range map[Bank]string{BankMercadoPago: "Mercado Pago", BankNubank: "Nubank", BankSicredi: "Sicredi"} {
		if b.Label() != want {
			t.Errorf("%s.Label() = %q, quer %q", b, b.Label(), want)
		}
	}
}

func TestCleanDescription_Edges(t *testing.T) {
	// corte em fronteira de runa: 250 runas multibyte ficam em até 200 e continuam UTF-8 válido
	got := CleanDescription(BankNubank, strings.Repeat("Ç", 250))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 200 {
		t.Errorf("corte inválido: %d runas, válido=%v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	// entrada só de hífens não vira vazia
	if got := CleanDescription(BankSicredi, "---"); got == "" {
		t.Error("\"---\" não pode virar vazio")
	}
	// Mercado Pago também respeita o limite
	mp := CleanDescription(BankMercadoPago, strings.Repeat("A", 300))
	if n := utf8.RuneCountInString(mp); n < 1 || n > 200 {
		t.Errorf("mercado pago: tamanho %d fora de 1..200", n)
	}
}

func TestCleanDescription_OnlyHyphensIsCapped(t *testing.T) {
	for _, b := range []Bank{BankNubank, BankSicredi, BankMercadoPago} {
		got := CleanDescription(b, strings.Repeat("- ", 150))
		if n := utf8.RuneCountInString(got); n < 1 || n > 200 {
			t.Errorf("%s: tamanho %d fora de 1..200", b, n)
		}
	}
}
