package models

import "time"

// Transaction representa um lançamento financeiro no banco de dados.
type Transaction struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	AmountCents int64     `json:"amount_cents"` // Valor em centavos (inteiro, sem erro de ponto flutuante)
	Type        string    `json:"type"`         // "income" (receita) ou "expense" (despesa)
	Category    string    `json:"category"`     // ex: Alimentação, Moradia, Salário, etc.
	Date        string    `json:"date"`         // Formato YYYY-MM-DD
	CreatedAt   time.Time `json:"created_at"`

	Installment *InstallmentInfo `json:"-"` // dados de exibição da parcela; não é gravado
}

// InstallmentInfo reúne os dados de exibição de um lançamento parcelado.
type InstallmentInfo struct {
	Number         int
	Total          int
	Remaining      int   // parcelas que ainda faltam depois desta
	RemainingCents int64 // Remaining x valor desta parcela
}

// Summary armazena os totais consolidados (em centavos) para exibição no dashboard.
type Summary struct {
	TotalIncome  int64
	TotalExpense int64
	Balance      int64
}

// PageData encapsula os dados enviados para a renderização do template HTML.
type PageData struct {
	SelectedMonth string // ex: "2026-10"
	DisplayMonth  string // ex: "Outubro de 2026"
	Summary       Summary
	Transactions  []Transaction
	TodayDate     string // Data atual para preencher o formulário por padrão
	ErrorMessage  string // Exibida no banner de alerta do template, quando preenchida
	Version       string // Versão da build, exibida no rodapé

	Projected        []ProjectedInstallment // parcelas previstas do mês (calculadas, não gravadas)
	ProjectedIncome  int64                  // soma das receitas previstas, em centavos
	ProjectedExpense int64                  // soma das despesas previstas, em centavos
	IncludeProjected bool                   // se os totais do mês somam as previstas (?previstas=1)
}

// ProjectedInstallment é uma parcela prevista: calculada a partir do histórico, nunca gravada.
type ProjectedInstallment struct {
	Name        string
	Type        string // "income" ou "expense"
	Number      int
	Total       int
	Date        string // AAAA-MM-DD
	AmountCents int64
}
