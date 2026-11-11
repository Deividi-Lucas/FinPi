// Package textutil reúne utilitários de texto compartilhados.
package textutil

import (
	"strings"
	"sync"
)

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c",
)

// Fold normaliza um texto para comparação: minúsculas, sem acentos e com espaços colapsados.
func Fold(s string) string {
	return strings.Join(strings.Fields(accents.Replace(strings.ToLower(s))), " ")
}

// maxFoldCache limita a memória de FoldCached: nomes de categoria são poucos, mas o texto vem do banco.
const maxFoldCache = 1024

var (
	foldMu    sync.RWMutex
	foldCache = make(map[string]string)
)

// FoldCached é o Fold memorizado, para textos curtos que se repetem muito (nomes de categoria, comparados a cada
// lançamento). Passado o limite, calcula sem guardar. Não use em descrições: são quase todas distintas.
func FoldCached(s string) string {
	foldMu.RLock()
	k, ok := foldCache[s]
	foldMu.RUnlock()
	if ok {
		return k
	}
	k = Fold(s)
	foldMu.Lock()
	if len(foldCache) < maxFoldCache {
		foldCache[s] = k
	}
	foldMu.Unlock()
	return k
}

func foldCacheLen() int {
	foldMu.RLock()
	defer foldMu.RUnlock()
	return len(foldCache)
}
