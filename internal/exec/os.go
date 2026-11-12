package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"
	"time"
)

// defaultMaxOutput é o teto de memória, por stream (stdout e stderr), quando MaxOutput é 0.
const defaultMaxOutput = 8 << 20

// OSRunner executa processos de verdade. Os testes não o usam para nada que mude o sistema.
//
// Garantias:
//   - Timeout/cancelamento encerram o PROCESSO e os descendentes (grupo de processos no Unix,
//     Job Object no Windows), não só o filho direto.
//   - Elevate no Unix usa "sudo -n": nunca pede senha. Sem credencial, devolve ErrNotElevated.
//   - Cmd.Env + Elevate no Unix vão por "sudo -n -- env VAR=valor cmd", porque o sudo descarta o ambiente.
//     Cmd.Dir vale para o sudo, que preserva o diretório de trabalho.
//   - stdout/stderr guardados em memória têm teto (MaxOutput); o excedente é descartado e Result.Truncated
//     vira true. O tee (Cmd.Stdout) recebe tudo.
type OSRunner struct {
	// GOOS e IsElevated são injetáveis para testar a lógica de elevação em qualquer SO.
	// Zero values: runtime.GOOS e "não elevado".
	GOOS       string
	IsElevated func() bool
	// Euid devolve o uid efetivo (nil: os.Geteuid; com GOOS injetado e Euid nil, "não é root"). Como root o
	// Elevate NÃO usa sudo; quem mostra o plano usa o mesmo Euid do runner real.
	Euid func() int
	// MaxOutput: teto, em bytes, de cada stream guardado em Result. 0 = 8 MiB.
	MaxOutput int
}

func (r OSRunner) goos() string {
	if r.GOOS != "" {
		return r.GOOS
	}
	return runtime.GOOS
}

func (r OSRunner) euid() int {
	switch {
	case r.Euid != nil:
		return r.Euid()
	case r.GOOS != "":
		return -1 // SO injetado nos testes e sem Euid: não é root
	}
	return os.Geteuid()
}

func (r OSRunner) maxOutput() int {
	if r.MaxOutput > 0 {
		return r.MaxOutput
	}
	return defaultMaxOutput
}

// CheckElevation devolve ErrNotElevated se c exige elevação e o processo está no Windows sem token
// elevado. No Unix a elevação é resolvida pelo sudo na execução, então não há o que checar antes.
func (r OSRunner) CheckElevation(c Cmd) error {
	if c.Elevate && r.goos() == "windows" && (r.IsElevated == nil || !r.IsElevated()) {
		return ErrNotElevated
	}
	return nil
}

// argv devolve o nome e os argumentos finais, aplicando a regra de elevação.
// viaSudo diz que o Env do Cmd já foi embutido nos argumentos (não vai para o ambiente do sudo).
func (r OSRunner) argv(c Cmd) (name string, args []string, viaSudo bool, err error) {
	if !c.Elevate {
		return c.Name, c.Args, false, nil
	}
	if err := r.CheckElevation(c); err != nil {
		return "", nil, false, err
	}
	if r.goos() == "windows" || r.euid() == 0 {
		return c.Name, c.Args, false, nil
	}
	args = []string{"-n", "--"}
	if len(c.Env) > 0 {
		args = append(args, "env")
		args = append(args, c.Env...)
	}
	args = append(args, c.Name)
	return "sudo", append(args, c.Args...), true, nil
}

// cappedBuffer guarda até max bytes e descarta o resto sem devolver erro (não derruba o processo).
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.max - b.buf.Len()
	switch {
	case room >= len(p):
		b.buf.Write(p)
	default:
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.truncated = true
	}
	return len(p), nil
}

// teeWriter repassa ao writer do chamador; se ele falhar, lembra o erro e segue aceitando os bytes,
// para o processo terminar normalmente e a falha ser reportada no fim.
type teeWriter struct {
	w   io.Writer
	err error
}

func (t *teeWriter) Write(p []byte) (int, error) {
	if t.err == nil {
		_, t.err = t.w.Write(p)
	}
	return len(p), nil
}

// ctxError traduz a expiração do ctx: o prazo do chamador (parent) sai como erro do ctx;
// ErrTimeout só quando estourou o Timeout do próprio Cmd (ctx derivado).
func ctxError(parent, derived context.Context, c Cmd) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if errors.Is(derived.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s", ErrTimeout, c)
	}
	return nil
}

// sudoRefused reconhece a recusa do próprio sudo -n (sem credencial, fora do sudoers): exit 1 e a
// mensagem prefixada por "sudo:" (o prefixo não é traduzido, ao contrário do texto).
func sudoRefused(code int, stderr []byte) bool {
	return code == 1 && strings.HasPrefix(string(stderr), "sudo: ")
}

// Run executa o comando e devolve o exit code bruto.
func (r OSRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	if err := validateEnv(c.Env); err != nil {
		return Result{}, err
	}
	name, args, viaSudo, err := r.argv(c)
	if err != nil {
		return Result{}, err
	}
	parent := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Dir = c.Dir
	if !viaSudo && len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdin = c.Stdin
	stdout := &cappedBuffer{max: r.maxOutput()}
	stderr := &cappedBuffer{max: r.maxOutput()}
	var out io.Writer = stdout
	var tee *teeWriter
	if c.Stdout != nil {
		tee = &teeWriter{w: c.Stdout}
		out = io.MultiWriter(stdout, tee)
	}
	cmd.Stdout, cmd.Stderr = out, stderr
	cmd.WaitDelay = 2 * time.Second
	group := &procGroup{}
	group.attach(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		if cerr := ctxError(parent, ctx, c); cerr != nil {
			return Result{}, cerr
		}
		return Result{}, fmt.Errorf("não foi possível iniciar %q: %w", c.Name, err)
	}
	group.started(cmd)
	waitErr := cmd.Wait()
	group.close()

	res := Result{
		Stdout:    stdout.buf.Bytes(),
		Stderr:    stderr.buf.Bytes(),
		Duration:  time.Since(start),
		Truncated: stdout.truncated || stderr.truncated,
	}
	if waitErr != nil && !errors.Is(waitErr, osexec.ErrWaitDelay) {
		if cerr := ctxError(parent, ctx, c); cerr != nil {
			return res, cerr
		}
		var exitErr *osexec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return res, fmt.Errorf("falha ao aguardar %q: %w", c.Name, waitErr)
		}
		res.ExitCode = exitErr.ExitCode()
	}
	if viaSudo && sudoRefused(res.ExitCode, res.Stderr) {
		return res, fmt.Errorf("%w: sudo recusou sem pedir senha (%s)", ErrNotElevated, strings.TrimSpace(string(res.Stderr)))
	}
	if tee != nil && tee.err != nil {
		return res, fmt.Errorf("falha ao escrever a saída ao vivo de %q: %w", c.Name, tee.err)
	}
	return res, nil
}

// LookPath procura o executável no PATH.
func (OSRunner) LookPath(name string) (string, error) { return osexec.LookPath(name) }

// Display devolve a linha de comando COMO SERÁ EXECUTADA, para o plano do dry-run: com Elevate no Unix,
// "sudo -n -- env VAR=valor ... cmd args"; sem Elevate mas com Env, "VAR=valor ... cmd args" (o Env vai para o
// ambiente do processo). Cmd com Secret não mostra os Args. Se a elevação for recusada (Windows sem token), cai
// em Cmd.String.
func (r OSRunner) Display(c Cmd) string {
	if c.Secret {
		return c.String()
	}
	name, args, viaSudo, err := r.argv(c)
	if err != nil {
		return c.String()
	}
	parts := make([]string, 0, len(args)+len(c.Env)+1)
	if !viaSudo {
		for _, e := range c.Env {
			parts = append(parts, quote(e))
		}
	}
	parts = append(parts, quote(name))
	for _, a := range args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}
