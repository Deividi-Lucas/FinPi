package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

var (
	ErrAccountNotFound  = errors.New("Conta não encontrada neste livro.")
	ErrAccountName      = errors.New("Informe o nome da conta.")
	ErrAccountDuplicate = errors.New("Já existe uma conta com esse nome neste livro.")
	ErrAccountArchived  = errors.New("A conta está arquivada.")
)

const createAccountsSQL = `
	CREATE TABLE IF NOT EXISTS accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		book_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		name_key TEXT NOT NULL,
		bank TEXT NOT NULL DEFAULT '',
		archived INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

const createAccountsIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_book_name ON accounts(book_id, name_key)`

const accountColumnSQL = `account_id INTEGER NOT NULL DEFAULT 0`

const accountColumns = `id, book_id, name, bank, archived`

func cleanAccountName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > maxCategoryNameLen {
		return "", ErrAccountName
	}
	return name, nil
}

// accountUniqueErr traduz a violação do índice único idx_accounts_book_name (corrida entre a checagem do nome e a
// gravação) para ErrAccountDuplicate; outros erros passam intactos.
func accountUniqueErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: accounts.") {
		return ErrAccountDuplicate
	}
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAccount(s rowScanner) (models.Account, error) {
	var (
		a        models.Account
		archived int
	)
	err := s.Scan(&a.ID, &a.BookID, &a.Name, &a.Bank, &archived)
	a.Archived = archived != 0
	return a, err
}

func queryAccounts(conn *sql.DB, query string, args ...any) ([]models.Account, error) {
	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// ListAccounts devolve as contas do livro: ativas por id, depois as arquivadas.
func (d *DB) ListAccounts() ([]models.Account, error) {
	return queryAccounts(d.conn, `SELECT `+accountColumns+` FROM accounts WHERE book_id = ? ORDER BY archived, id`, d.book)
}

// ListAccountsAll devolve as contas de todos os livros (filtro de Todos os livros).
func (d *DB) ListAccountsAll() ([]models.Account, error) {
	return queryAccounts(d.conn, `SELECT `+accountColumns+` FROM accounts ORDER BY book_id, archived, id`)
}

// UsedAccountIDs devolve as contas (de todos os livros) que têm ao menos um lançamento.
func (d *DB) UsedAccountIDs() (map[int64]bool, error) {
	rows, err := d.conn.Query(`SELECT DISTINCT account_id FROM transactions WHERE account_id <> 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		used[id] = true
	}
	return used, rows.Err()
}

// GetAccount devolve uma conta do livro, ou ErrAccountNotFound.
func (d *DB) GetAccount(id int64) (models.Account, error) {
	a, err := scanAccount(d.conn.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ? AND book_id = ?`, id, d.book))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrAccountNotFound
	}
	return a, err
}

func (d *DB) accountNameTaken(key string, exceptID int64) (bool, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name_key = ? AND id <> ?`, d.book, key, exceptID).Scan(&n)
	return n > 0, err
}

// CreateAccount cria uma conta no livro. bank é "" ou uma das constantes models.Bank*.
func (d *DB) CreateAccount(name, bank string) (models.Account, error) {
	clean, err := cleanAccountName(name)
	if err != nil {
		return models.Account{}, err
	}
	key := textutil.Fold(clean)
	taken, err := d.accountNameTaken(key, 0)
	if err != nil {
		return models.Account{}, err
	}
	if taken {
		return models.Account{}, ErrAccountDuplicate
	}
	res, err := d.conn.Exec(`INSERT INTO accounts (book_id, name, name_key, bank) VALUES (?, ?, ?, ?)`, d.book, clean, key, bank)
	if err != nil {
		return models.Account{}, accountUniqueErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Account{}, err
	}
	return models.Account{ID: id, BookID: d.book, Name: clean, Bank: bank}, nil
}

// RenameAccount renomeia uma conta do livro.
func (d *DB) RenameAccount(id int64, name string) error {
	clean, err := cleanAccountName(name)
	if err != nil {
		return err
	}
	if _, err := d.GetAccount(id); err != nil {
		return err
	}
	key := textutil.Fold(clean)
	taken, err := d.accountNameTaken(key, id)
	if err != nil {
		return err
	}
	if taken {
		return ErrAccountDuplicate
	}
	_, err = d.conn.Exec(`UPDATE accounts SET name = ?, name_key = ? WHERE id = ? AND book_id = ?`, clean, key, id, d.book)
	return accountUniqueErr(err)
}

// SetAccountArchived arquiva ou reativa uma conta do livro (contas nunca são apagadas).
func (d *DB) SetAccountArchived(id int64, archived bool) error {
	if _, err := d.GetAccount(id); err != nil {
		return err
	}
	v := 0
	if archived {
		v = 1
	}
	_, err := d.conn.Exec(`UPDATE accounts SET archived = ? WHERE id = ? AND book_id = ?`, v, id, d.book)
	return err
}

// AccountForBank devolve a conta ativa de menor id do livro ligada a esse banco.
func (d *DB) AccountForBank(bank string) (models.Account, bool, error) {
	a, err := scanAccount(d.conn.QueryRow(`SELECT `+accountColumns+` FROM accounts
		WHERE book_id = ? AND bank = ? AND archived = 0 ORDER BY id LIMIT 1`, d.book, bank))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Account{}, false, nil
	}
	return a, err == nil, err
}

// GetOrCreateBankAccount reaproveita a conta ativa do banco ou cria uma com o nome do banco
// (acrescentando " (2)", " (3)"… se o nome já estiver em uso por outra conta).
func (d *DB) GetOrCreateBankAccount(bank string) (models.Account, error) {
	if a, ok, err := d.AccountForBank(bank); err != nil || ok {
		return a, err
	}
	base := models.BankName(bank)
	if base == "" {
		return models.Account{}, ErrAccountName
	}
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		taken, err := d.accountNameTaken(textutil.Fold(name), 0)
		if err != nil {
			return models.Account{}, err
		}
		if !taken {
			return d.CreateAccount(name, bank)
		}
	}
}

// migrateAccounts cria a tabela de contas e a coluna account_id e, uma única vez (PRAGMA user_version = 3),
// atribui os lançamentos importados à conta do banco de origem, pelo prefixo do external_id.
func migrateAccounts(conn *sql.DB) error {
	if _, err := conn.Exec(createAccountsSQL); err != nil {
		return err
	}
	if _, err := conn.Exec(createAccountsIndexSQL); err != nil {
		return err
	}
	if err := ensureColumn(conn, "transactions", "account_id", accountColumnSQL); err != nil {
		return err
	}
	var version int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 3 {
		return nil
	}

	banks := []struct{ prefix, bank string }{
		{"mp:", models.BankMercadoPago}, {"nu:", models.BankNubank},
		{"sic:", models.BankSicredi}, {"bb:", models.BankBB},
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type pair struct {
		book         int64
		bank, prefix string
	}
	var pairs []pair
	for _, b := range banks {
		// Coleta antes de gravar: a conexão é única.
		rows, err := tx.Query(`SELECT DISTINCT book_id FROM transactions WHERE external_id LIKE ? ORDER BY book_id`, b.prefix+"%")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			pairs = append(pairs, pair{id, b.bank, b.prefix})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	for _, p := range pairs {
		name := models.BankName(p.bank)
		var id int64
		err := tx.QueryRow(`SELECT id FROM accounts WHERE book_id = ? AND bank = ? AND archived = 0 ORDER BY id LIMIT 1`, p.book, p.bank).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			// Nome livre no livro (pode já existir uma conta sem banco com o mesmo nome).
			for i := 1; ; i++ {
				candidate := name
				if i > 1 {
					candidate = fmt.Sprintf("%s (%d)", name, i)
				}
				var n int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name_key = ?`, p.book, textutil.Fold(candidate)).Scan(&n); err != nil {
					return err
				}
				if n == 0 {
					res, err := tx.Exec(`INSERT INTO accounts (book_id, name, name_key, bank) VALUES (?, ?, ?, ?)`,
						p.book, candidate, textutil.Fold(candidate), p.bank)
					if err != nil {
						return err
					}
					if id, err = res.LastInsertId(); err != nil {
						return err
					}
					break
				}
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE transactions SET account_id = ? WHERE book_id = ? AND external_id LIKE ?`,
			id, p.book, p.prefix+"%"); err != nil {
			return err
		}
	}
	// Dentro da transação: a atribuição e a marca de versão são gravadas juntas (ou nenhuma).
	if _, err := tx.Exec(`PRAGMA user_version = 3`); err != nil {
		return err
	}
	return tx.Commit()
}
