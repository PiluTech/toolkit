// Package animacoes reduz as animações de janela do Windows (parte B da função de otimização), MOSTRANDO antes o que
// muda (valor atual -> novo), guardando o valor ORIGINAL de cada item (inclusive "não existia") em backup antes da
// primeira escrita e desfazendo exatamente (tipo e conteúdo; só apaga o que não existia).
//
// Escopo fechado e por usuário: só valores do HKCU, nenhum administrador, nenhum serviço, tarefa ou HKLM, nenhuma
// máscara binária (UserPreferencesMask). A função é EXPERIMENTAL enquanto os valores não forem confirmados em VM.
//
// Todo acesso ao Windows passa pela interface System; os testes usam uma implementação em memória.
package animacoes

import (
	"context"

	"github.com/PiluTech/toolkit/internal/diagfmt"
	"github.com/PiluTech/toolkit/internal/privacidade"
)

// Lang é o idioma da saída (o mesmo do diagfmt).
type Lang = diagfmt.Lang

// RegValue é um valor do registro (tipo preservado): o mesmo da privacidade.
type RegValue = privacidade.RegValue

// DWORD cria um valor numérico.
func DWORD(n uint32) RegValue { return privacidade.DWORD(n) }

// System é o registro do USUÁRIO (HKCU) visto pelo toolkit. As chaves vêm no formato "HKCU\\Software\\...". Nenhuma
// operação exige administrador, e a implementação real recusa qualquer chave fora do HKCU.
type System interface {
	// RegRead lê um valor; exists=false só quando a ausência está PROVADA (não é erro).
	RegRead(ctx context.Context, key, name string) (v RegValue, exists bool, err error)
	RegWrite(ctx context.Context, key, name string, v RegValue) error
	// RegDelete apaga o valor (não a chave); apagar o que não existe não é erro.
	RegDelete(ctx context.Context, key, name string) error
}
