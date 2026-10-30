package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"finpi/internal/models"

	_ "modernc.org/sqlite" // Driver SQLite 100% puro Go sem CGO
)

// ErrNotFound indica que nenhum lançamento corresponde ao ID informado.
var ErrNotFound = errors.New("lançamento não encontrado")

type DB struct {
	conn *sql.DB
	now  func() time.Time // relógio (trocável nos testes); usado para datar os lotes de edição
	book int64            // livro em que as consultas atuam (ver ForBook)
}

// InitDB inicializa o banco SQLite com pragmas otimizados para baixo consumo de RAM e durabilidade do cartão SD do Raspberry Pi.
func InitDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir banco de dados sqlite: %w", err)
	}

	// SQLite trabalha de forma ideal com conexão única em sistemas embarcados:
	// evita contenção de locks de escrita e reduz drasticamente o consumo de RAM.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	// Pragmas essenciais para Raspberry Pi (protege o cartão microSD e melhora concorrência)
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",   // Write-Ahead Logging: reduz escritas síncronas no SD
		"PRAGMA synchronous = NORMAL;", // Seguro com WAL, evita fsync agressivo que desgasta o flash
		"PRAGMA busy_timeout = 5000;",  // Aguarda até 5s em caso de bloqueio antes de falhar
		"PRAGMA cache_size = -2000;",   // Limita o cache em memória a ~2MB (reduz uso de RAM)
		"PRAGMA temp_store = MEMORY;",  // Tabelas temporárias leves em RAM em vez de disco
	}

	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("erro ao aplicar pragma '%s': %w", pragma, err)
		}
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro ao aplicar schema inicial: %w", err)
	}

	return &DB{conn: conn, now: time.Now, book: DefaultBookID}, nil
}

const createTransactionsSQL = `
	CREATE TABLE %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		description TEXT NOT NULL,
		amount_cents INTEGER NOT NULL,
		type TEXT NOT NULL CHECK(type IN ('income', 'expense')),
		category TEXT NOT NULL,
		date TEXT NOT NULL,
		priority TEXT NOT NULL DEFAULT '' CHECK(priority IN ('', 'essential', 'superfluous')),
		external_id TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

const createIndexSQL = `CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(date)`

// priorityColumnSQL é a definição da coluna de classificação usada no ALTER de bancos antigos.
const priorityColumnSQL = `priority TEXT NOT NULL DEFAULT '' CHECK(priority IN ('', 'essential', 'superfluous'))`

// ensureColumn acrescenta uma coluna a bancos antigos. Idempotente e sem alterar dados.
// definition é a definição completa (nome incluído), ex.: "priority TEXT NOT NULL DEFAULT ''".
func ensureColumn(conn *sql.DB, table, column, definition string) error {
	cols, err := tableColumns(conn, table)
	if err != nil {
		return err
	}
	if cols[column] {
		return nil
	}
	_, err = conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s`, table, definition))
	return err
}

// externalIDColumnSQL guarda o identificador de origem de lançamentos importados ("" = lançado à mão).
const externalIDColumnSQL = `external_id TEXT NOT NULL DEFAULT ''`

// Índice único parcial: o mesmo lançamento importado não entra duas vezes, mesmo com envios simultâneos.
const createExternalIDIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_external_id ON transactions(external_id) WHERE external_id <> ''`

func migrate(conn *sql.DB) error {
	cols, err := tableColumns(conn, "transactions")
	if err != nil {
		return err
	}

	switch {
	case len(cols) == 0:
		// Banco novo.
		if _, err := conn.Exec(fmt.Sprintf(createTransactionsSQL, "transactions")); err != nil {
			return err
		}
	case cols["amount"] && !cols["amount_cents"]:
		// Banco da versão antiga (valor em REAL): converte para centavos em uma única transação.
		if err := migrateRealToCents(conn); err != nil {
			return fmt.Errorf("erro ao converter valores para centavos: %w", err)
		}
	}

	if _, err := conn.Exec(createIndexSQL); err != nil {
		return err
	}
	if _, err := conn.Exec(fmt.Sprintf(categoriesTableSQL, "IF NOT EXISTS categories")); err != nil {
		return err
	}
	for _, table := range []string{"transactions", "categories"} {
		if err := ensureColumn(conn, table, "priority", priorityColumnSQL); err != nil {
			return fmt.Errorf("erro ao adicionar a coluna priority em %s: %w", table, err)
		}
	}
	if err := ensureColumn(conn, "transactions", "external_id", externalIDColumnSQL); err != nil {
		return fmt.Errorf("erro ao adicionar a coluna external_id: %w", err)
	}
	if _, err := conn.Exec(createExternalIDIndexSQL); err != nil {
		return fmt.Errorf("erro ao criar o índice de external_id: %w", err)
	}
	for _, stmt := range []string{createBatchesSQL, createBatchItemsSQL} {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("erro ao criar as tabelas de edição em lote: %w", err)
		}
	}
	if _, err := conn.Exec(createBooksSQL); err != nil {
		return fmt.Errorf("erro ao criar a tabela de livros: %w", err)
	}
	if _, err := conn.Exec(`INSERT OR IGNORE INTO books (id, name, name_key, color) VALUES (1, 'Pessoal', 'pessoal', 'emerald')`); err != nil {
		return fmt.Errorf("erro ao criar o livro Pessoal: %w", err)
	}
	for _, table := range []string{"transactions", "batches"} {
		if err := ensureColumn(conn, table, "book_id", bookColumnSQL); err != nil {
			return fmt.Errorf("erro ao adicionar book_id em %s: %w", table, err)
		}
	}
	if err := ensureColumn(conn, "transactions", "original_description", originalDescriptionColumnSQL); err != nil {
		return fmt.Errorf("erro ao adicionar a coluna original_description: %w", err)
	}
	if err := migrateCategoriesPerBook(conn); err != nil {
		return fmt.Errorf("erro ao separar as categorias por livro: %w", err)
	}
	if _, err := conn.Exec(`CREATE INDEX IF NOT EXISTS idx_transactions_book_date ON transactions(book_id, date)`); err != nil {
		return err
	}
	return seedCategories(conn)
}

// DefaultBookID é o livro "Pessoal", criado na migração; recebe tudo o que existia antes dos livros.
const DefaultBookID = 1

const createBooksSQL = `
	CREATE TABLE IF NOT EXISTS books (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		name_key TEXT NOT NULL UNIQUE,
		color TEXT NOT NULL DEFAULT 'emerald',
		archived INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

const bookColumnSQL = `book_id INTEGER NOT NULL DEFAULT 1`

// originalDescriptionColumnSQL guarda o texto original do extrato; vazio nos lançamentos antigos e manuais.
const originalDescriptionColumnSQL = "original_description TEXT NOT NULL DEFAULT ''"

// migrateCategoriesPerBook reconstrói a tabela categories (formato antigo: name_key UNIQUE no banco todo)
// com book_id e UNIQUE(book_id, name_key). Tudo numa transação; os dados ficam no livro 1.
func migrateCategoriesPerBook(conn *sql.DB) error {
	cols, err := tableColumns(conn, "categories")
	if err != nil {
		return err
	}
	if cols["book_id"] {
		return nil
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []string{
		fmt.Sprintf(categoriesTableSQL, "categories_new"),
		`INSERT INTO categories_new (id, book_id, name, name_key, hidden, priority)
		 SELECT id, 1, name, name_key, hidden, priority FROM categories`,
		`DROP TABLE categories`,
		`ALTER TABLE categories_new RENAME TO categories`,
	}
	for _, stmt := range steps {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ForBook devolve um acesso ao banco que atua só no livro informado (compartilha a conexão).
func (d *DB) ForBook(id int64) *DB {
	c := *d
	c.book = id
	return &c
}

// BookID é o livro em que este acesso atua.
func (d *DB) BookID() int64 { return d.book }

func tableColumns(conn *sql.DB, table string) (map[string]bool, error) {
	rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func migrateRealToCents(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	steps := []string{
		fmt.Sprintf(createTransactionsSQL, "transactions_new"),
		`INSERT INTO transactions_new (id, description, amount_cents, type, category, date, created_at)
		 SELECT id, description, CAST(ROUND(amount * 100) AS INTEGER), type, category, date, created_at
		 FROM transactions`,
		`DROP TABLE transactions`,
		`ALTER TABLE transactions_new RENAME TO transactions`,
	}
	for _, stmt := range steps {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// ErrInvalidPriority indica uma classificação fora de '', 'essential' e 'superfluous'.
var ErrInvalidPriority = errors.New("prioridade inválida: use essential, superfluous ou vazio")

// transactionColumns é a lista de colunas lidas por scanTransactions.
const transactionColumns = `id, description, amount_cents, type, category, date, priority, created_at, original_description`

func scanTransactions(rows *sql.Rows) ([]models.Transaction, error) {
	var list []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.Description, &t.AmountCents, &t.Type, &t.Category, &t.Date, &t.Priority, &t.CreatedAt, &t.OriginalDescription); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// CreateTransaction insere um novo lançamento.
func (d *DB) CreateTransaction(t *models.Transaction) error {
	if !models.ValidPriority(t.Priority) {
		return ErrInvalidPriority
	}
	query := `
		INSERT INTO transactions (description, amount_cents, type, category, date, priority, created_at, book_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	res, err := d.conn.Exec(query, t.Description, t.AmountCents, t.Type, t.Category, t.Date, t.Priority, now, d.book)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		t.ID = id
	}
	return nil
}

// GetTransactions recupera lançamentos ordenados do mais recente para o mais antigo.
// Se yearMonth estiver preenchido (ex: "2026-10"), filtra por esse mês.
func (d *DB) GetTransactions(yearMonth string) ([]models.Transaction, error) {
	var query string
	var args []interface{}

	if yearMonth != "" {
		query = `
			SELECT ` + transactionColumns + `
			FROM transactions
			WHERE book_id = ? AND strftime('%Y-%m', date) = ?
			ORDER BY date DESC, id DESC
		`
		args = append(args, d.book, yearMonth)
	} else {
		query = `
			SELECT ` + transactionColumns + `
			FROM transactions
			WHERE book_id = ?
			ORDER BY date DESC, id DESC
			LIMIT 100
		`
		args = append(args, d.book)
	}

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTransactions(rows)
}

// GetTransactionsBetween recupera os lançamentos com data de from a to (inclusive, AAAA-MM-DD),
// do mais antigo para o mais recente.
func (d *DB) GetTransactionsBetween(from, to string) ([]models.Transaction, error) {
	rows, err := d.conn.Query(`
		SELECT `+transactionColumns+`
		FROM transactions
		WHERE book_id = ? AND date >= ? AND date <= ?
		ORDER BY date ASC, id ASC
	`, d.book, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTransactions(rows)
}

// GetInstallmentCandidates devolve os lançamentos cuja descrição pode conter uma parcela
// ("3/10" ou "parcela 3 de 10"), do mais antigo para o mais recente. É um pré-filtro barato:
// a validação do formato é feita em installments.Parse.
func (d *DB) GetInstallmentCandidates() ([]models.Transaction, error) {
	rows, err := d.conn.Query(`
		SELECT ` + transactionColumns + `
		FROM transactions
		WHERE book_id = ? AND (description LIKE '%/%' OR description LIKE '%parcela%')
		ORDER BY date ASC, id ASC
	`, d.book)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTransactions(rows)
}

// GetSummaryByMonth calcula Total de Receitas, Total de Despesas e Saldo (em centavos) para o mês fornecido.
func (d *DB) GetSummaryByMonth(yearMonth string) (models.Summary, error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'income' THEN amount_cents ELSE 0 END), 0) AS total_income,
			COALESCE(SUM(CASE WHEN type = 'expense' THEN amount_cents ELSE 0 END), 0) AS total_expense
		FROM transactions
		WHERE book_id = ? AND strftime('%Y-%m', date) = ?
	`
	var s models.Summary
	row := d.conn.QueryRow(query, d.book, yearMonth)
	if err := row.Scan(&s.TotalIncome, &s.TotalExpense); err != nil {
		return s, err
	}
	s.Balance = s.TotalIncome - s.TotalExpense
	return s, nil
}

// UpdateTransaction atualiza os dados de um lançamento existente pelo ID.
func (d *DB) UpdateTransaction(t *models.Transaction) error {
	if !models.ValidPriority(t.Priority) {
		return ErrInvalidPriority
	}
	query := `
		UPDATE transactions
		SET description = ?, amount_cents = ?, type = ?, category = ?, date = ?, priority = ?
		WHERE id = ? AND book_id = ?
	`
	res, err := d.conn.Exec(query, t.Description, t.AmountCents, t.Type, t.Category, t.Date, t.Priority, t.ID, d.book)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteTransaction exclui um lançamento pelo ID.
func (d *DB) DeleteTransaction(id int64) error {
	res, err := d.conn.Exec(`DELETE FROM transactions WHERE id = ? AND book_id = ?`, id, d.book)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
