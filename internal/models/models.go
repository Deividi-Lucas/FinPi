package models

import "time"

// Transaction representa um lançamento financeiro no banco de dados.
type Transaction struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Amount      float64   `json:"amount"`
	Type        string    `json:"type"`     // "income" (receita) ou "expense" (despesa)
	Category    string    `json:"category"` // ex: Alimentação, Moradia, Salário, etc.
	Date        string    `json:"date"`     // Formato YYYY-MM-DD
	CreatedAt   time.Time `json:"created_at"`
}

// Summary armazena os totais consolidados para exibição no dashboard.
type Summary struct {
	TotalIncome  float64
	TotalExpense float64
	Balance      float64
}

// PageData encapsula os dados enviados para a renderização do template HTML.
type PageData struct {
	SelectedMonth string        // ex: "2026-10"
	DisplayMonth  string        // ex: "Outubro de 2026"
	Summary       Summary
	Transactions  []Transaction
	TodayDate     string        // Data atual para preencher o formulário por padrão
	ErrorMessage  string
}
