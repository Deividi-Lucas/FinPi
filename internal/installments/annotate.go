package installments

import "finpi/internal/models"

// Annotate preenche o selo (N/M) e o restante dos lançamentos cuja descrição traz uma parcela válida.
func Annotate(txs []models.Transaction) {
	for i := range txs {
		info, ok := Parse(txs[i].Description)
		if !ok {
			continue
		}
		left := info.Total - info.Number
		txs[i].Installment = &models.InstallmentInfo{
			Number:         info.Number,
			Total:          info.Total,
			Remaining:      left,
			RemainingCents: int64(left) * txs[i].AmountCents,
		}
	}
}
