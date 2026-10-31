# Segurança

## Versões suportadas

O projeto está em desenvolvimento e ainda não tem release. Quando houver, só a última release recebe
correções de segurança.

## Como reportar uma vulnerabilidade

Não abra issue pública para falha de segurança.

Use o reporte privado do GitHub: aba **Security** do repositório → **Report a vulnerability**
(<https://github.com/PiluVitu/toolkit/security/advisories/new>). Inclua:

- a versão ou o commit afetado e o SO;
- os passos para reproduzir, o resultado esperado e o obtido;
- o impacto que você enxerga (por exemplo: download sem verificação, execução de comando não declarado,
  escalada de privilégio, segredo no log).

O projeto é mantido por uma pessoa, então a resposta é por melhor esforço. A correção e a divulgação
são combinadas com quem reportou.

## Regras que o toolkit garante

Qualquer caso em que uma destas regras não se cumpra é uma vulnerabilidade:

1. **Nenhum download sem SHA-256.** O binário (via bootstrap), apps `url`, apps `github-release` e
   scripts usados por receitas são conferidos antes de executar ou instalar. Sem hash conhecido, a
   operação é recusada, sem opção para pular.
   - `url`: `sha256` obrigatório no catálogo.
   - `github-release`: `checksums.txt` da release, ou o `digest` publicado pelo GitHub, ou `sha256`
     fixado no catálogo.
   - Bootstraps: `checksums.txt` da mesma release do binário.
   - O arquivo baixado fica num diretório temporário e só é usado depois que o hash confere.
2. **Nenhum `curl | bash` de terceiros** dentro de receitas.
3. **Extração segura**: arquivos zip e tar com caminho absoluto, `..` ou link simbólico para fora do
   destino são recusados.
4. **Privilégio mínimo**: `sudo` só nos comandos que precisam e elevação no Windows só quando algo
   escolhido exige admin.
5. **Logs sem segredos**: `GITHUB_TOKEN`, cabeçalhos `Authorization` e comandos marcados como secretos
   são mascarados.
6. **Hash de pacote do winget nunca é ignorado**: `--ignore-security-hash` não é usado em hipótese alguma.

## Fora do escopo

- Vulnerabilidades nos próprios gerenciadores (winget, Homebrew, apt, dnf, pacman, Flatpak) ou nos
  apps instalados: reporte ao projeto de origem.
- Pacotes maliciosos publicados pelo próprio dono de um repositório de origem. O hash protege contra
  adulteração no caminho, não contra um autor mal-intencionado. Abra uma issue comum se um app do
  catálogo deve sair.
