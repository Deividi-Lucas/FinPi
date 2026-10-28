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
}
