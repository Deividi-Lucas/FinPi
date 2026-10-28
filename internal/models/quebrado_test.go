package models

import "testing"

// Teste quebrado de propósito para validar que o CI bloqueia o merge. NÃO MERGEAR.
func TestQuebradoDeProposito(t *testing.T) {
	t.Fatal("falha intencional para testar a proteção da main")
}
