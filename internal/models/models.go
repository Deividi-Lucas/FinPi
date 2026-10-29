package models

import (
	"strconv"
	"time"
)

// Transaction representa um lançamento financeiro no banco de dados.
type Transaction struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	AmountCents int64     `json:"amount_cents"` // Valor em centavos (inteiro, sem erro de ponto flutuante)
	Type        string    `json:"type"`         // "income" (receita) ou "expense" (despesa)
	Category    string    `json:"category"`     // ex: Alimentação, Moradia, Salário, etc.
	Date        string    `json:"date"`         // Formato YYYY-MM-DD
	CreatedAt   time.Time `json:"created_at"`

	Priority   string `json:"priority"` // "", "essential" ou "superfluous"; vazio segue a categoria
	ExternalID string `json:"-"`        // identificador de origem (importação); usado só na gravação em lote
	BookID     int64  `json:"-"`        // livro do lançamento; preenchido onde o livro importa (lotes e movimentação)

	Installment       *InstallmentInfo `json:"-"` // dados de exibição da parcela; não é gravado
	EffectivePriority string           `json:"-"` // classificação efetiva (própria ou da categoria); não é gravada
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

	Categories     []Category // categorias visíveis (listas de escolha ao lançar)
	AllCategories  []Category // todas, ocultas inclusive (pop-up de categorias)
	ShowCategories bool       // abre o pop-up de categorias ao carregar
	CategoryError  string     // erro exibido dentro do pop-up de categorias

	Stats MonthStats // números do dashboard do mês

	ImportResult *ImportResult // aviso após importar um extrato (nil = sem aviso)
	BulkResult   *BulkResult   // aviso após uma edição em lote (nil = sem aviso)
	Undo         *UndoView     // edição em lote que ainda pode ser desfeita (nil = nenhuma)

	// Histórico filtrado (a lista de lançamentos); os cartões e o dashboard do mês continuam usando Transactions.
	History          []Transaction
	HistoryCount     int   // lançamentos que passaram pelos filtros (antes do limite de exibição)
	HistoryIncome    int64 // soma das receitas da lista filtrada, em centavos
	HistoryExpense   int64 // soma das despesas da lista filtrada, em centavos
	HistoryTruncated bool  // a lista mostra só os mais recentes
	Filter           FilterView

	Book     Book   // livro selecionado
	BookID   int64  // id do livro selecionado
	Books    []Book // livros ativos
	AllBooks []Book // todos, para o cadastro
	BookPath string // página atual, para o seletor de livro

	BookError  string      // erro exibido dentro do pop-up de livros
	ShowBooks  bool        // abre o pop-up de livros ao carregar
	BookColors []BookColor // paleta para os formulários do cadastro de livros
}

// FilterView são os filtros da lista, prontos para o formulário e para os campos ocultos.
type FilterView struct {
	Text       string
	Type       string          // "", "receita" ou "despesa" (valor da URL)
	Min, Max   string          // "12,50" ou ""
	AllMonths  bool            // procurar em todos os meses
	Active     int             // quantos filtros estão ligados
	Query      string          // parâmetros normalizados (sem "?"); "" sem filtros
	Warning    string          // avisos de valores inválidos
	Categories []string        // categorias marcadas, como vieram
	Selected   map[string]bool // categoria (nome exibido) → marcada
}

// ImportResult é o resultado de uma importação, exibido num aviso após o redirecionamento.
type ImportResult struct {
	Imported int
	Skipped  int
}

// BulkResult é o resultado de uma operação em lote, exibido num aviso após o redirecionamento.
type BulkResult struct {
	Kind    string // "edit", "delete" ou "undo"
	Count   int
	Skipped int
}

// UndoView descreve a edição em lote que pode ser desfeita.
type UndoView struct {
	Action  string // "edit" ou "delete"
	Summary string
	Count   int
	At      string // data e hora local, já formatada
}

// TransactionKey identifica um lançamento por data, valor (módulo, em centavos) e tipo.
// Usada para achar possíveis duplicados entre lançamentos manuais e linhas de extrato.
func TransactionKey(date string, cents int64, typ string) string {
	return date + "|" + strconv.FormatInt(cents, 10) + "|" + typ
}

// ProjectedInstallment é uma parcela prevista: calculada a partir do histórico, nunca gravada.
type ProjectedInstallment struct {
	Name        string
	Category    string // categoria da parcela mais recente da série (usada pelos filtros)
	Type        string // "income" ou "expense"
	Number      int
	Total       int
	Date        string // AAAA-MM-DD
	AmountCents int64
}

// Category é uma categoria de lançamentos.
type Category struct {
	ID     int64
	Name   string
	Hidden bool // oculta: não aparece na lista de escolha ao lançar
	Usage  int  // quantidade de lançamentos que usam a categoria

	Priority string // "", "essential" ou "superfluous"
}

// Classificação das despesas.
const (
	PriorityEssential   = "essential"   // não se vive sem
	PrioritySuperfluous = "superfluous" // se vive sem
)

// ValidPriority aceita "" (sem classificação / segue a categoria), "essential" e "superfluous".
func ValidPriority(p string) bool {
	return p == "" || p == PriorityEssential || p == PrioritySuperfluous
}

// PriorityGroup soma as despesas de uma classificação no mês.
type PriorityGroup struct {
	Key     string  // "essential", "superfluous" ou "unclassified"
	Label   string  // texto da tela
	Cents   int64   // soma em centavos
	Count   int     // quantidade de despesas
	Percent float64 // do total de despesas, com uma casa decimal
}

// MonthStats reúne os números do dashboard do mês (calculados, nunca gravados).
type MonthStats struct {
	TotalCount   int
	IncomeCount  int
	ExpenseCount int
	ExpenseTotal int64
	Largest      *Transaction    // maior despesa lançada (nil sem despesas)
	Smallest     *Transaction    // menor despesa lançada (nil sem despesas)
	Groups       []PriorityGroup // essencial, supérfluo, sem classificação (nessa ordem)
}

// Essential, Superfluous e Unclassified devolvem cada grupo (valor zero se Groups estiver vazio).
func (s MonthStats) Essential() PriorityGroup    { return s.group(0) }
func (s MonthStats) Superfluous() PriorityGroup  { return s.group(1) }
func (s MonthStats) Unclassified() PriorityGroup { return s.group(2) }

func (s MonthStats) group(i int) PriorityGroup {
	if i < len(s.Groups) {
		return s.Groups[i]
	}
	return PriorityGroup{}
}

// Book é um livro: um conjunto separado de lançamentos e categorias (ex.: Pessoal, PJ).
type Book struct {
	ID       int64
	Name     string
	Color    string // chave de BookColors
	Archived bool
}

// BookColor é uma cor disponível para um livro.
type BookColor struct{ Key, Label, Hex string }

// BookColors é a paleta fixa de cores dos livros; a primeira é a padrão.
var BookColors = []BookColor{
	{"emerald", "Verde", "#10b981"},
	{"sky", "Azul", "#0ea5e9"},
	{"violet", "Violeta", "#8b5cf6"},
	{"amber", "Âmbar", "#f59e0b"},
	{"rose", "Rosa", "#f43f5e"},
	{"slate", "Cinza", "#94a3b8"},
}

// ValidBookColor diz se key é uma cor da paleta.
func ValidBookColor(key string) bool {
	for _, c := range BookColors {
		if c.Key == key {
			return true
		}
	}
	return false
}

// Hex devolve a cor do livro em #rrggbb (a padrão se a chave for desconhecida).
func (b Book) Hex() string {
	for _, c := range BookColors {
		if c.Key == b.Color {
			return c.Hex
		}
	}
	return BookColors[0].Hex
}
