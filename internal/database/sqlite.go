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

	return &DB{conn: conn}, nil
}

const createTransactionsSQL = `
	CREATE TABLE %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		description TEXT NOT NULL,
		amount_cents INTEGER NOT NULL,
		type TEXT NOT NULL CHECK(type IN ('income', 'expense')),
		category TEXT NOT NULL,
		date TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

const createIndexSQL = `CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(date)`

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
	if _, err := conn.Exec(createCategoriesSQL); err != nil {
		return err
	}
	return seedCategories(conn)
}

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

// CreateTransaction insere um novo lançamento.
func (d *DB) CreateTransaction(t *models.Transaction) error {
	query := `
		INSERT INTO transactions (description, amount_cents, type, category, date, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	res, err := d.conn.Exec(query, t.Description, t.AmountCents, t.Type, t.Category, t.Date, now)
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
			SELECT id, description, amount_cents, type, category, date, created_at
			FROM transactions
			WHERE strftime('%Y-%m', date) = ?
			ORDER BY date DESC, id DESC
		`
		args = append(args, yearMonth)
	} else {
		query = `
			SELECT id, description, amount_cents, type, category, date, created_at
			FROM transactions
			ORDER BY date DESC, id DESC
			LIMIT 100
		`
	}

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.Description, &t.AmountCents, &t.Type, &t.Category, &t.Date, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}

	return list, rows.Err()
}

// GetInstallmentCandidates devolve os lançamentos cuja descrição pode conter uma parcela
// ("3/10" ou "parcela 3 de 10"), do mais antigo para o mais recente. É um pré-filtro barato:
// a validação do formato é feita em installments.Parse.
func (d *DB) GetInstallmentCandidates() ([]models.Transaction, error) {
	rows, err := d.conn.Query(`
		SELECT id, description, amount_cents, type, category, date, created_at
		FROM transactions
		WHERE description LIKE '%/%' OR description LIKE '%parcela%'
		ORDER BY date ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.Description, &t.AmountCents, &t.Type, &t.Category, &t.Date, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// GetSummaryByMonth calcula Total de Receitas, Total de Despesas e Saldo (em centavos) para o mês fornecido.
func (d *DB) GetSummaryByMonth(yearMonth string) (models.Summary, error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'income' THEN amount_cents ELSE 0 END), 0) AS total_income,
			COALESCE(SUM(CASE WHEN type = 'expense' THEN amount_cents ELSE 0 END), 0) AS total_expense
		FROM transactions
		WHERE strftime('%Y-%m', date) = ?
	`
	var s models.Summary
	row := d.conn.QueryRow(query, yearMonth)
	if err := row.Scan(&s.TotalIncome, &s.TotalExpense); err != nil {
		return s, err
	}
	s.Balance = s.TotalIncome - s.TotalExpense
	return s, nil
}

// UpdateTransaction atualiza os dados de um lançamento existente pelo ID.
func (d *DB) UpdateTransaction(t *models.Transaction) error {
	query := `
		UPDATE transactions
		SET description = ?, amount_cents = ?, type = ?, category = ?, date = ?
		WHERE id = ?
	`
	res, err := d.conn.Exec(query, t.Description, t.AmountCents, t.Type, t.Category, t.Date, t.ID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteTransaction exclui um lançamento pelo ID.
func (d *DB) DeleteTransaction(id int64) error {
	res, err := d.conn.Exec(`DELETE FROM transactions WHERE id = ?`, id)
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
