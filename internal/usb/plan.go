package usb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PiluTech/toolkit/internal/textsafe"
)

// Textos fixos que valem em TODO fluxo (plano, CLI, TUI e docs): o rótulo de experimental e o limite honesto.
const (
	TextExperimental  = "EXPERIMENTAL: esta função ainda não foi executada numa máquina real (A VERIFICAR em VM, ver docs/usb.md). Nada se aplica por padrão e nada muda sem a sua confirmação."
	TextLimit         = "Limite honesto: um dispositivo que CLONA o VID/PID de um permitido PASSA; a política nega só as duas classes de USB, então um dispositivo HID novo cujo nó não é de classe USB nem USBDevice (um teclado simples, por exemplo) pode NÃO ser bloqueado (A VERIFICAR em VM); a política só controla a instalação ou atualização de driver de dispositivo novo (o que já está instalado continua) e não é defesa contra quem tem acesso físico e conhece os IDs. Isto reduz o risco, não o elimina."
	TextNeverElevates = "O toolkit nunca eleva sozinho: abra o terminal como administrador (botão direito, \"Executar como administrador\")."
)

// RecoverySteps é o roteiro de recuperação mostrado na confirmação, no plano e quando o estado original é desconhecido.
// Os comandos exatos do WinRE e do Modo Seguro são A VERIFICAR em VM (docs/usb.md).
var RecoverySteps = []string{
	"Se o teclado travar mesmo assim: (1) espere o prazo da reversão automática; (2) use outro teclado já permitido; (3) entre com outra conta de administrador e restaure o .reg exportado em " + ProtectedDir + " (só apague a chave " + RestrictionsKey + " no Editor de Registro ou no gpedit.msc depois de confirmar que não havia política anterior: GPO de domínio, MDM ou outra ferramenta); (4) Modo Seguro (se a política vale nele: A VERIFICAR); (5) Ambiente de Recuperação do Windows (WinRE): prompt de comando, reg load do hive SOFTWARE offline, restaurar o .reg ou, só se confirmado que não havia política anterior, apagar a chave, reg unload (comandos A VERIFICAR em VM).",
}

// Refusal é um motivo de recusa do plano; Kind é o erro sentinela que a representa.
type refusal struct {
	kind error
	text string
}

// Plan é o que `aplicar` faria: política, dispositivos, prova e recusas. Só leitura.
type Plan struct {
	Edition  string
	IsAdmin  bool
	Prazo    int
	Policy   Policy
	Units    []Unit
	Proof    Proof
	Refusals []string
	refusals []refusal
}

// OK diz se o plano pode ser aplicado (sem recusas). Administrador não entra aqui: é conferido no Apply.
func (p *Plan) OK() bool { return len(p.refusals) == 0 }

// Err devolve o erro do plano recusado (nil se OK), com o sentinela do PRIMEIRO motivo e todos os textos.
func (p *Plan) Err() error {
	if p.OK() {
		return nil
	}
	return fmt.Errorf("%w: %s", p.refusals[0].kind, strings.Join(p.Refusals, "; "))
}

func (p *Plan) refuse(kind error, text string) {
	p.refusals = append(p.refusals, refusal{kind, text})
	p.Refusals = append(p.Refusals, text)
}

// EditionSupported diz se a edição está na documentação da política (Pro, Enterprise e Education); Home e o resto são recusados.
func EditionSupported(ed string) bool {
	return ed == EditionPro || ed == EditionEnterprise || ed == EditionEducation
}

// namespacePresent diz se a lista fechada de alvos do toolkit tem algum valor presente.
func namespacePresent(tr RegTree) bool {
	for k := range tr.Entries {
		if checkRel(k) == nil {
			return true
		}
	}
	return false
}

// BuildPlan lê (só leitura, sem administrador) a edição, o registro e o PnP e monta o plano com a PROVA. Falha de leitura é
// erro (nunca "provavelmente seguro"); tudo que impede aplicar vira recusa no plano.
func BuildPlan(ctx context.Context, sys System, excluded []string) (*Plan, error) {
	ed, err := sys.Edition(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para descobrir a edição do Windows: %w", err)
	}
	p := &Plan{Edition: ed, IsAdmin: sys.IsAdmin(ctx), Prazo: PrazoPadrao}
	if !EditionSupported(ed) {
		p.refuse(ErrEdition, fmt.Sprintf("a edição %q do Windows não consta na documentação da política (só Pro, Enterprise e Education): o toolkit recusa em vez de prometer o que a Microsoft não documenta", textsafe.Line(ed)))
	}
	tr, err := sys.ReadTree(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para ler a chave Restrictions: %w", err)
	}
	if len(tr.Entries) > 0 || len(tr.Subkeys) > 0 {
		p.refuse(ErrExistingPolicy, "já existe política de restrição de instalação de dispositivos nesta máquina (de outra origem ou de uma aplicação anterior); o toolkit não mistura com ela. Desfaça a anterior (toolkit usb desfazer) ou peça a quem administra a máquina")
	}
	devs, err := sys.Devices(ctx)
	if err != nil {
		return nil, fmt.Errorf("não deu para ler os dispositivos (PnP): %w", err)
	}
	an, err := Analyze(devs, excluded)
	if err != nil {
		if errors.Is(err, ErrTreeIncomplete) || errors.Is(err, ErrNoProof) {
			p.refuse(ErrNoProof, "sem prova dos dispositivos essenciais: "+textsafe.Line(err.Error()))
			return p, nil
		}
		return nil, err
	}
	p.Policy, p.Units, p.Proof = an.Policy, an.Units, an.Proof
	if !an.Proof.OK {
		for _, f := range an.Proof.Failures {
			p.refuse(ErrNoProof, textsafe.Line(f))
		}
	}
	return p, nil
}

// Format escreve o plano em texto (determinístico). Todo texto de dispositivo já veio limpo por ParseDevices.
func (p *Plan) Format() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("Modo empresa: permitir só os USB conhecidos (Windows) [EXPERIMENTAL]")
	w("%s", TextExperimental)
	w("")
	w("%s", TextLimit)
	w("")
	adm := "não (aplicar exige administrador)"
	if p.IsAdmin {
		adm = "sim"
	}
	w("Edição do Windows: %s · administrador: %s", textsafe.Line(p.Edition), adm)
	w("")
	w("Dispositivos USB conectados agora (marcados = entram na lista e ficam PERMITIDOS; desmarcados = fora da lista, e bloqueados ao reconectar só se a política os alcançar):")
	if len(p.Units) == 0 {
		w("  (nenhum dispositivo USB externo)")
	}
	for _, u := range p.Units {
		mark, state := "[x]", "permitido"
		switch {
		case !u.Selected && u.Blocked:
			mark, state = "[ ]", "BLOQUEADO ao reconectar (desmarcado)"
		case !u.Selected:
			mark, state = "[ ]", fmt.Sprintf("desmarcado e fora da lista, mas a política NÃO o bloquearia (a classe %s não está na negação: A VERIFICAR em VM)", classOrDash(u.Class))
		}
		ess := ""
		if u.Essential {
			ess = " · essencial (teclado, mouse ou outro HID)"
		}
		w("  %s %s  %s  classe %s  conectado agora%s -> %s", mark, u.Name, u.VIDPID, classOrDash(u.Class), ess, state)
	}
	w("")
	w("O que a política grava (só %s):", RestrictionsKey)
	w("  - ordem em camadas (AllowDenyLayered), em vez da negação global de tudo que não está na lista (que valeria para TODOS os barramentos)")
	w("  - %d ID(s) de hardware permitidos: os VID/PID dos USB marcados e %d ID(s) genéricos de controlador e hub raiz", len(p.Policy.AllowIDs), len(genericAllowIDs))
	w("  - negação só das 2 classes de USB (%s, %s); NUNCA classe de teclado, mouse, HID ou disco", denyClassGUIDs[0], denyClassGUIDs[1])
	w("")
	if p.OK() {
		w("Prova de dispositivos essenciais: OK (nenhum fica bloqueado, nem os pais deles)")
	} else {
		w("Prova de dispositivos essenciais: RECUSADO, nada será alterado")
	}
	for _, l := range p.Proof.Lines {
		sym := "OK "
		if !l.Allowed {
			sym = "NÃO"
		}
		w("  %s %s (%s, %s): %s", sym, l.Name, classOrDash(l.Class), l.Bus, l.By)
	}
	for _, r := range p.Refusals {
		w("RECUSADO: %s", r)
	}
	w("")
	w("Ordem do aplicar: prova -> backup protegido em %s -> tarefa de reversão criada e conferida -> só então a primeira chave. Falha em qualquer passo = nenhuma alteração.", ProtectedDir)
	w("Reversão automática em %d minuto(s): se você não confirmar (toolkit usb confirmar), a tarefa restaura o snapshot sozinha (execução como SYSTEM sem sessão: a provar em VM).", p.Prazo)
	w("")
	for _, s := range RecoverySteps {
		w("%s", s)
	}
	return b.String()
}

func classOrDash(c string) string {
	if c == "" {
		return "-"
	}
	return c
}
