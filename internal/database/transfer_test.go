package database

import (
	"errors"
	"testing"

	"finpi/internal/models"
	"finpi/internal/transfer"
)

// pair cria uma saída no Pessoal e uma entrada no PJ do mesmo valor.
func pair(t *testing.T, pessoal, pj *DB, cents int64, date string) (out, in models.Transaction) {
	t.Helper()
	out = mustCreate(t, pessoal, models.Transaction{Description: "Pix para PJ", AmountCents: cents, Type: "expense", Category: "Outros", Date: date})
	in = mustCreate(t, pj, models.Transaction{Description: "Pix do Pessoal", AmountCents: cents, Type: "income", Category: "Outros", Date: date})
	return out, in
}

func TestLinkTransfer_LinksAndListsValid(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 8190, "2026-10-05")
	if err := pessoal.LinkTransfer(out.ID, in.ID); err != nil {
		t.Fatal(err)
	}
	links, err := pessoal.ListTransferLinks()
	if err != nil || len(links) != 1 || !links[0].Valid || links[0].Out.ID != out.ID || links[0].In.ID != in.ID {
		t.Fatalf("ListTransferLinks = %+v, %v", links, err)
	}
	if links[0].Out.BookID != pessoal.BookID() || links[0].In.BookID != pj.BookID() {
		t.Errorf("os livros dos lados vieram errados: %+v", links[0])
	}
}

func TestLinkTransfer_Validation(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 5000, "2026-10-05")
	other := mustCreate(t, pj, models.Transaction{Description: "Outro", AmountCents: 5000, Type: "income", Category: "Outros", Date: "2026-10-06"})
	sameBook := mustCreate(t, pessoal, models.Transaction{Description: "Mesmo livro", AmountCents: 5000, Type: "income", Category: "Outros", Date: "2026-10-05"})
	diff := mustCreate(t, pj, models.Transaction{Description: "Valor diferente", AmountCents: 4999, Type: "income", Category: "Outros", Date: "2026-10-05"})
	if err := pessoal.LinkTransfer(out.ID, in.ID); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		outID, inID int64
		want        error
	}{
		{"já ligado (saída)", out.ID, other.ID, transfer.ErrTaken},
		{"já ligado (entrada)", sameBook.ID, in.ID, transfer.ErrNeedOutIn},
		{"mesmo livro", out.ID, sameBook.ID, transfer.ErrSameBook}, // a validação do par vem antes da checagem de "já ligado"
		{"inexistente", 9999, in.ID, transfer.ErrNotFound},
		{"valor diferente", mustCreate(t, pessoal, models.Transaction{Description: "Outra saída", AmountCents: 5000, Type: "expense", Category: "Outros", Date: "2026-10-05"}).ID, diff.ID, transfer.ErrAmount},
	}
	for _, c := range cases {
		if err := pessoal.LinkTransfer(c.outID, c.inID); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, quer %v", c.name, err, c.want)
		}
	}
	if err := pessoal.LinkTransfer(in.ID, out.ID); !errors.Is(err, transfer.ErrNeedOutIn) {
		t.Errorf("ordem trocada: %v", err)
	}
}

func TestLinkTransfer_SameBookIsRejected(t *testing.T) {
	pessoal, _ := twoBooks(t)
	out := mustCreate(t, pessoal, models.Transaction{Description: "s", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	in := mustCreate(t, pessoal, models.Transaction{Description: "e", AmountCents: 100, Type: "income", Category: "Outros", Date: "2026-10-05"})
	if err := pessoal.LinkTransfer(out.ID, in.ID); !errors.Is(err, transfer.ErrSameBook) {
		t.Errorf("mesmo livro: %v", err)
	}
}

func TestUnlinkTransfer(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 100, "2026-10-05")
	if err := pessoal.LinkTransfer(out.ID, in.ID); err != nil {
		t.Fatal(err)
	}
	links, _ := pessoal.ListTransferLinks()
	if err := pessoal.UnlinkTransfer(links[0].ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := pessoal.ListTransferLinks(); len(links) != 0 {
		t.Errorf("a ligação deveria ter sumido: %+v", links)
	}
	if err := pessoal.UnlinkTransfer(9999); !errors.Is(err, transfer.ErrNotFound) {
		t.Errorf("desligar de novo: %v", err)
	}
}

func TestDeletingATransactionRemovesItsLinkAndMark(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 100, "2026-10-05")
	if err := pessoal.LinkTransfer(out.ID, in.ID); err != nil {
		t.Fatal(err)
	}
	if err := pessoal.DeleteTransaction(out.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := pessoal.ListTransferLinks(); len(links) != 0 {
		t.Errorf("excluir a saída deveria apagar a ligação: %+v", links)
	}
	// em lote, a marca também some
	solo := mustCreate(t, pj, models.Transaction{Description: "Solo", AmountCents: 50, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	if marked, _, err := pessoal.MarkSolo([]int64{solo.ID}); err != nil || marked != 1 {
		t.Fatalf("MarkSolo = %d, %v", marked, err)
	}
	if _, _, err := pj.ApplyDeletes("Exclusão", []int64{solo.ID}); err != nil {
		t.Fatal(err)
	}
	if list, _ := pessoal.ListSolo(); len(list) != 0 {
		t.Errorf("a marca deveria ter sumido: %+v", list)
	}
}

func TestEditingTheAmountInvalidatesTheLinkAndFreesTheTransaction(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 5000, "2026-10-05")
	if err := pessoal.LinkTransfer(out.ID, in.ID); err != nil {
		t.Fatal(err)
	}
	out.AmountCents = 5100
	if err := pessoal.UpdateTransaction(&out); err != nil {
		t.Fatal(err)
	}
	links, _ := pessoal.ListTransferLinks()
	if len(links) != 1 || links[0].Valid {
		t.Fatalf("a ligação deveria estar inválida: %+v", links)
	}
	// a saída (valor novo) pode ser ligada a outra entrada: a ligação inválida é limpa antes
	in2 := mustCreate(t, pj, models.Transaction{Description: "Entrada nova", AmountCents: 5100, Type: "income", Category: "Outros", Date: "2026-10-05"})
	if err := pessoal.LinkTransfer(out.ID, in2.ID); err != nil {
		t.Fatalf("depois da limpeza deveria ligar: %v", err)
	}
	if links, _ := pessoal.ListTransferLinks(); len(links) != 1 || !links[0].Valid || links[0].In.ID != in2.ID {
		t.Errorf("ListTransferLinks = %+v", links)
	}
}

func TestMarkAndUnmarkSolo(t *testing.T) {
	pessoal, pj := twoBooks(t)
	a := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	b := mustCreate(t, pj, models.Transaction{Description: "B", AmountCents: 200, Type: "income", Category: "Outros", Date: "2026-10-05"})
	marked, skipped, err := pessoal.MarkSolo([]int64{a.ID, b.ID, a.ID, 9999})
	if err != nil || marked != 2 || skipped != 1 { // 9999 não existe; o repetido conta uma vez
		t.Fatalf("MarkSolo = %d marcados, %d ignorados, %v", marked, skipped, err)
	}
	list, _ := pessoal.ListSolo()
	if len(list) != 2 {
		t.Fatalf("ListSolo = %+v", list)
	}
	if marked, skipped, _ := pessoal.MarkSolo([]int64{a.ID}); marked != 0 || skipped != 1 {
		t.Errorf("marcar de novo: %d, %d", marked, skipped)
	}
	if n, err := pessoal.UnmarkSolo([]int64{a.ID, 9999}); err != nil || n != 1 {
		t.Errorf("UnmarkSolo = %d, %v", n, err)
	}
	if list, _ := pessoal.ListSolo(); len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("ListSolo = %+v", list)
	}
}

func TestSoloAndLinkExcludeEachOther(t *testing.T) {
	pessoal, pj := twoBooks(t)
	out, in := pair(t, pessoal, pj, 100, "2026-10-05")
	if _, _, err := pessoal.MarkSolo([]int64{out.ID}); err != nil {
		t.Fatal(err)
	}
	if err := pessoal.LinkTransfer(out.ID, in.ID); !errors.Is(err, transfer.ErrTaken) {
		t.Errorf("ligar um lançamento marcado: %v", err)
	}
}

func TestAllTransactions_SkipsArchivedBooksAndFlagsTruncation(t *testing.T) {
	pessoal, pj := twoBooks(t)
	mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	mustCreate(t, pj, models.Transaction{Description: "B", AmountCents: 2, Type: "income", Category: "Outros", Date: "2026-10-06"})
	all, truncated, err := pessoal.AllTransactions(10)
	if err != nil || len(all) != 2 || truncated {
		t.Fatalf("AllTransactions = %+v, %v, %v", all, truncated, err)
	}
	if all[0].BookID == 0 || all[1].BookID == 0 || all[0].BookID == all[1].BookID {
		t.Errorf("BookID deveria vir preenchido: %+v", all)
	}
	if _, truncated, _ := pessoal.AllTransactions(1); !truncated {
		t.Error("com limite 1 e 2 lançamentos deveria sinalizar truncamento")
	}
	if err := pessoal.SetBookArchived(pj.BookID(), true); err != nil {
		t.Fatal(err)
	}
	if all, _, _ := pessoal.AllTransactions(10); len(all) != 1 {
		t.Errorf("livro arquivado deveria ficar de fora: %+v", all)
	}
}
