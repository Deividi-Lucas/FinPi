package database

import (
	"database/sql"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

// migrateNubankRDBInternal move os Resgate/Aplicação RDB do Nubank já importados para a categoria
// Movimentação interna (as novas importações já vêm assim). Roda uma única vez (PRAGMA user_version = 2),
// para não desfazer uma recategorização que o usuário faça depois.
func migrateNubankRDBInternal(conn *sql.DB) error {
	var version int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 2 {
		return nil
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const match = `external_id LIKE 'nu:%' AND (description LIKE 'Resgate RDB%' OR description LIKE 'Aplica%o RDB%')`
	key := textutil.Fold(models.InternalCategory)
	if _, err := tx.Exec(`INSERT OR IGNORE INTO categories (book_id, name, name_key)
		SELECT DISTINCT book_id, ?, ? FROM transactions WHERE `+match, models.InternalCategory, key); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE transactions SET category = ? WHERE `+match+` AND category <> ?`,
		models.InternalCategory, models.InternalCategory); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = conn.Exec(`PRAGMA user_version = 2`)
	return err
}
