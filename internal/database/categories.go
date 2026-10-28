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
		hidden INTEGER NOT NULL DEFAULT 0
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

	rows, err := d.conn.Query(`SELECT id, name, name_key, hidden FROM categories ORDER BY name_key`)
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
		if err := rows.Scan(&c.ID, &c.Name, &key, &hidden); err != nil {
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
