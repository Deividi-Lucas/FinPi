package database

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

var (
	ErrBookNotFound     = errors.New("livro não encontrado")
	ErrBookExists       = errors.New("já existe um livro com esse nome")
	ErrInvalidBookName  = errors.New("nome de livro inválido: use de 1 a 40 caracteres")
	ErrInvalidBookColor = errors.New("cor inválida")
	ErrLastBook         = errors.New("é preciso manter ao menos um livro ativo")
)

func cleanBookName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > maxCategoryNameLen {
		return "", ErrInvalidBookName
	}
	return name, nil
}

// ListBooks devolve todos os livros, por id (os arquivados também).
func (d *DB) ListBooks() ([]models.Book, error) {
	rows, err := d.conn.Query(`SELECT id, name, color, archived FROM books ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Book
	for rows.Next() {
		var (
			b        models.Book
			archived int
		)
		if err := rows.Scan(&b.ID, &b.Name, &b.Color, &archived); err != nil {
			return nil, err
		}
		b.Archived = archived != 0
		list = append(list, b)
	}
	return list, rows.Err()
}

// GetBook devolve um livro pelo id, ou ErrBookNotFound.
func (d *DB) GetBook(id int64) (models.Book, error) {
	var (
		b        models.Book
		archived int
	)
	err := d.conn.QueryRow(`SELECT id, name, color, archived FROM books WHERE id = ?`, id).Scan(&b.ID, &b.Name, &b.Color, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrBookNotFound
	}
	b.Archived = archived != 0
	return b, err
}

// CreateBook cria um livro. Com copyFrom > 0, copia as categorias (nome, visibilidade e prioridade) desse livro;
// com 0, o livro começa sem categorias.
func (d *DB) CreateBook(name, color string, copyFrom int64) (models.Book, error) {
	clean, err := cleanBookName(name)
	if err != nil {
		return models.Book{}, err
	}
	if !models.ValidBookColor(color) {
		return models.Book{}, ErrInvalidBookColor
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return models.Book{}, err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM books WHERE name_key = ?`, textutil.Fold(clean)).Scan(&n); err != nil {
		return models.Book{}, err
	}
	if n > 0 {
		return models.Book{}, ErrBookExists
	}
	if copyFrom > 0 {
		if err := tx.QueryRow(`SELECT COUNT(*) FROM books WHERE id = ?`, copyFrom).Scan(&n); err != nil {
			return models.Book{}, err
		}
		if n == 0 {
			return models.Book{}, ErrBookNotFound
		}
	}
	res, err := tx.Exec(`INSERT INTO books (name, name_key, color) VALUES (?, ?, ?)`, clean, textutil.Fold(clean), color)
	if err != nil {
		return models.Book{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Book{}, err
	}
	if copyFrom > 0 {
		if _, err := tx.Exec(`INSERT INTO categories (book_id, name, name_key, hidden, priority)
			SELECT ?, name, name_key, hidden, priority FROM categories WHERE book_id = ?`, id, copyFrom); err != nil {
			return models.Book{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Book{}, err
	}
	return models.Book{ID: id, Name: clean, Color: color}, nil
}

// UpdateBook renomeia o livro e troca a cor.
func (d *DB) UpdateBook(id int64, name, color string) error {
	clean, err := cleanBookName(name)
	if err != nil {
		return err
	}
	if !models.ValidBookColor(color) {
		return ErrInvalidBookColor
	}
	if _, err := d.GetBook(id); err != nil {
		return err
	}
	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM books WHERE name_key = ? AND id <> ?`, textutil.Fold(clean), id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrBookExists
	}
	_, err = d.conn.Exec(`UPDATE books SET name = ?, name_key = ?, color = ? WHERE id = ?`, clean, textutil.Fold(clean), color, id)
	return err
}

// SetBookArchived arquiva ou reativa o livro. Nunca deixa o banco sem livro ativo.
func (d *DB) SetBookArchived(id int64, archived bool) error {
	b, err := d.GetBook(id)
	if err != nil {
		return err
	}
	if !archived {
		_, err := d.conn.Exec(`UPDATE books SET archived = 0 WHERE id = ?`, id)
		return err
	}
	if b.Archived {
		return nil
	}
	// Numa única instrução, para que duas chamadas simultâneas não arquivem os dois últimos livros ativos.
	res, err := d.conn.Exec(`UPDATE books SET archived = 1 WHERE id = ?
		AND (SELECT COUNT(*) FROM books WHERE archived = 0 AND id <> ?) > 0`, id, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLastBook
	}
	return nil
}
