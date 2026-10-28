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

const maxCategoryNameLen = 40

var (
	ErrCategoryExists      = errors.New("já existe uma categoria com esse nome")
	ErrCategoryNotFound    = errors.New("categoria não encontrada")
	ErrInvalidCategoryName = errors.New("nome de categoria inválido: use de 1 a 40 caracteres")
)

// CategoryInUseError indica que a categoria não pode ser excluída por ter lançamentos.
type CategoryInUseError struct {
	Name  string
	Count int
}

func (e *CategoryInUseError) Error() string {
	noun := "lançamentos"
	if e.Count == 1 {
		noun = "lançamento"
	}
	return fmt.Sprintf("%q está em uso por %d %s; renomeie ou oculte", e.Name, e.Count, noun)
}

var defaultCategories = []string{
	"Alimentação", "Moradia", "Transporte", "Salário", "Lazer",
	"Saúde", "Investimentos", "Educação", "Outros",
}

const createCategoriesSQL = `
	CREATE TABLE IF NOT EXISTS categories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		name_key TEXT NOT NULL UNIQUE,
		hidden INTEGER NOT NULL DEFAULT 0,
		priority TEXT NOT NULL DEFAULT '' CHECK(priority IN ('', 'essential', 'superfluous'))
	)`

// cleanCategoryName remove espaços das pontas, colapsa os internos e valida o tamanho (1 a 40 caracteres).
func cleanCategoryName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > maxCategoryNameLen {
		return "", ErrInvalidCategoryName
	}
	return name, nil
}

// queryer é satisfeito por *sql.DB e *sql.Tx.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// usage reúne os textos exatos usados em lançamentos para uma mesma categoria normalizada e o total.
type usage struct {
	variants []string
	count    int
}

// categoryUsage agrupa os lançamentos por categoria normalizada (textutil.Fold).
func categoryUsage(q queryer) (map[string]*usage, error) {
	rows, err := q.Query(`SELECT category, COUNT(*) FROM transactions GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]*usage{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		key := textutil.Fold(name)
		u := out[key]
		if u == nil {
			u = &usage{}
			out[key] = u
		}
		u.variants = append(u.variants, name)
		u.count += n
	}
	return out, rows.Err()
}

// seedCategories popula a tabela uma única vez (controlada por PRAGMA user_version): as categorias
// padrão e as já usadas por lançamentos existentes. Nunca recria categorias excluídas depois.
func seedCategories(conn *sql.DB) error {
	var version int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}

	names := append([]string{}, defaultCategories...)
	rows, err := conn.Query(`SELECT DISTINCT category FROM transactions ORDER BY category`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range names {
		name = strings.Join(strings.Fields(name), " ")
		if name == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (name, name_key) VALUES (?, ?)`, name, textutil.Fold(name)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = conn.Exec(`PRAGMA user_version = 1`)
	return err
}

// ListCategories devolve todas as categorias (ocultas inclusive), em ordem alfabética sem acentos.
func (d *DB) ListCategories() ([]models.Category, error) {
	use, err := categoryUsage(d.conn)
	if err != nil {
		return nil, err
	}

	rows, err := d.conn.Query(`SELECT id, name, name_key, hidden, priority FROM categories ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Category
	for rows.Next() {
		var (
			c      models.Category
			key    string
			hidden int
		)
		if err := rows.Scan(&c.ID, &c.Name, &key, &hidden, &c.Priority); err != nil {
			return nil, err
		}
		c.Hidden = hidden != 0
		if u := use[key]; u != nil {
			c.Usage = u.count
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// CategoryExists diz se existe categoria com esse nome (ignora caixa e acentos; ocultas contam).
func (d *DB) CategoryExists(name string) (bool, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM categories WHERE name_key = ?`, textutil.Fold(name)).Scan(&n)
	return n > 0, err
}

// CreateCategory cria uma categoria nova. Devolve ErrCategoryExists se o nome (normalizado) já existir.
func (d *DB) CreateCategory(name string) (models.Category, error) {
	clean, err := cleanCategoryName(name)
	if err != nil {
		return models.Category{}, err
	}
	exists, err := d.CategoryExists(clean)
	if err != nil {
		return models.Category{}, err
	}
	if exists {
		return models.Category{}, ErrCategoryExists
	}

	res, err := d.conn.Exec(`INSERT INTO categories (name, name_key) VALUES (?, ?)`, clean, textutil.Fold(clean))
	if err != nil {
		return models.Category{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Category{}, err
	}
	return models.Category{ID: id, Name: clean}, nil
}

// GetOrCreateCategory devolve a categoria existente (mesmo oculta) ou cria uma nova.
func (d *DB) GetOrCreateCategory(name string) (models.Category, error) {
	clean, err := cleanCategoryName(name)
	if err != nil {
		return models.Category{}, err
	}

	var (
		c      models.Category
		hidden int
	)
	err = d.conn.QueryRow(`SELECT id, name, hidden, priority FROM categories WHERE name_key = ?`, textutil.Fold(clean)).
		Scan(&c.ID, &c.Name, &hidden, &c.Priority)
	switch {
	case err == nil:
		c.Hidden = hidden != 0
		return c, nil
	case errors.Is(err, sql.ErrNoRows):
		return d.CreateCategory(clean)
	default:
		return models.Category{}, err
	}
}

// RenameCategory renomeia a categoria e todos os lançamentos que a usam, tudo numa única transação.
func (d *DB) RenameCategory(id int64, name string) error {
	clean, err := cleanCategoryName(name)
	if err != nil {
		return err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldKey string
	err = tx.QueryRow(`SELECT name_key FROM categories WHERE id = ?`, id).Scan(&oldKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCategoryNotFound
	}
	if err != nil {
		return err
	}

	newKey := textutil.Fold(clean)
	var others int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM categories WHERE name_key = ? AND id <> ?`, newKey, id).Scan(&others); err != nil {
		return err
	}
	if others > 0 {
		return ErrCategoryExists
	}

	use, err := categoryUsage(tx)
	if err != nil {
		return err
	}
	if u := use[oldKey]; u != nil {
		for _, variant := range u.variants {
			if _, err := tx.Exec(`UPDATE transactions SET category = ? WHERE category = ?`, clean, variant); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE categories SET name = ?, name_key = ? WHERE id = ?`, clean, newKey, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetCategoryHidden oculta ou reexibe a categoria na lista de escolha ao lançar.
func (d *DB) SetCategoryHidden(id int64, hidden bool) error {
	flag := 0
	if hidden {
		flag = 1
	}
	res, err := d.conn.Exec(`UPDATE categories SET hidden = ? WHERE id = ?`, flag, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

// SetCategoryPriority define a classificação da categoria ("" remove). Valor fora da lista dá ErrInvalidPriority.
func (d *DB) SetCategoryPriority(id int64, priority string) error {
	if !models.ValidPriority(priority) {
		return ErrInvalidPriority
	}
	res, err := d.conn.Exec(`UPDATE categories SET priority = ? WHERE id = ?`, priority, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

// DeleteCategory exclui uma categoria sem lançamentos. Em uso, devolve *CategoryInUseError.
func (d *DB) DeleteCategory(id int64) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var name, key string
	err = tx.QueryRow(`SELECT name, name_key FROM categories WHERE id = ?`, id).Scan(&name, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCategoryNotFound
	}
	if err != nil {
		return err
	}

	use, err := categoryUsage(tx)
	if err != nil {
		return err
	}
	if u := use[key]; u != nil && u.count > 0 {
		return &CategoryInUseError{Name: name, Count: u.count}
	}

	if _, err := tx.Exec(`DELETE FROM categories WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
