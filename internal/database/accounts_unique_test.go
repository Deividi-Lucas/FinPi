package database

import (
	"errors"
	"testing"
)

func TestAccountUniqueErr_MapsUniqueViolation(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.CreateAccount("Carteira", ""); err != nil {
		t.Fatal(err)
	}
	// INSERT direto, sem a checagem prévia: simula a corrida entre a checagem e a gravação.
	_, raw := db.conn.Exec(`INSERT INTO accounts (book_id, name, name_key, bank) VALUES (?, 'CARTEIRA', 'carteira', '')`, db.BookID())
	if raw == nil {
		t.Fatal("esperava violação do índice único")
	}
	if err := accountUniqueErr(raw); !errors.Is(err, ErrAccountDuplicate) {
		t.Errorf("accountUniqueErr(%v) = %v, quer ErrAccountDuplicate", raw, err)
	}
	other := errors.New("disco cheio")
	if err := accountUniqueErr(other); err != other {
		t.Errorf("outro erro deveria passar intacto: %v", err)
	}
	if err := accountUniqueErr(nil); err != nil {
		t.Errorf("nil = %v", err)
	}
}
