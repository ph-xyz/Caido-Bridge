# Changelog

## Unreleased

- Made `scopeId` explicit for local scope checks, previews, and active Replay;
  only the selected preset is evaluated, deny rules take precedence, and empty
  allowlists fail closed.
- Added Caido-compatible `?` host wildcards alongside `*`.
- Bound every active execution to a cryptographically random, two-minute,
  one-use preview token covering project, request, scope, source fingerprint,
  and prepared request fingerprint.
- Replaced unconditional session/redaction claims with factual fields that
  distinguish present authentication material, sensitive-header redaction, and
  the absence of request-body redaction.
- Added complete Go dependency notices/license texts, Dependabot configuration,
  and regression tests for conflicting scopes, token mismatch, reuse, and
  expiry.

## v0.4.0

- Renamed the public project, executable, version output, source command, and
  MCP implementation identity to `CaidoBridge` / `CaidoBridge.exe`. The
  v0.3.1 state directory, tunnel profile, and scheduled task remain only for
  upgrade compatibility.
- Added a guided, transactional clean installer with preflight validation,
  DPAPI credential storage, ACL protection, idempotent upgrades, and rollback.
- Added root-level doctor, status, Replay control, token update, and uninstall
  wrappers.
- Flattened the Go source tree into the repository root and changed the module
  path to `github.com/ph-xyz/Caido-Bridge`.
- Added English and Brazilian Portuguese documentation, CI, tag-triggered
  release packaging, repository hygiene checks, and local release artifacts.
- Kept Active Replay disabled by default and preserved all v0.3.1 request,
  project, scope, origin, fingerprint, confirmation, and redaction controls.

## v0.3.1

- Removido o request budget cumulativo/hardcoded de três envios ativos.
- Removidos `requestBudget` dos outputs MCP, a constante interna e os textos
  que induziam o modelo a encerrar uma hunt ao atingir um contador arbitrário.
- Removido o teto de três mutações explícitas; múltiplas mutações continuam
  exigindo `allowMultipleMutations=true`.
- Mantido o comportamento auditável por chamada: um envio em
  `caido_replay_request`, um teste com baseline histórico ou controle + teste
  com baseline ao vivo em `caido_test_hypothesis`.
- Mantidos projeto/ID/fingerprint/scope/same-host, preview, confirmação de
  execução, confirmação state-changing, redaction e diff factual.
- Adicionadas instruções explícitas para continuar testes sequenciais orientados
  por evidência até concluir o objetivo, receber ordem de parada ou encontrar
  outro bloqueio real.

## v0.3.0

- Adicionada `caido_preview_replay`, sempre read-only e sem tráfego de alvo.
- Adicionadas, somente com opt-in explícito, `caido_replay_request` e
  `caido_test_hypothesis`.
- Implementado o fluxo Replay do Caido 0.57+ com draft, task, polling e captura
  de request/response reais.
- Adicionados guardas de projeto, ID visível, method+host+path, fingerprint,
  consistência raw/metadata, scope, Host imutável, confirmação e budget.
- Adicionadas mutações estruturadas de método, path, query, headers, cookies,
  JSON e body, com one-mutation rule por padrão.
- Adicionados baseline histórico/live, diff objetivo de response e evidence
  bundle sem verdict de vulnerabilidade.
- Adicionada habilitação Windows explícita e reversível por
  `scripts/set-replay.ps1`, preservando DPAPI e o perfil do túnel.
- Mantidos os sete contratos de leitura da v0.2.4 e adicionados testes para
  imutabilidade, isolamento de mutação, ID, host, scope, baseline, diff,
  redaction e request budget.

## v0.2.4

- Corrigido globalmente o ID publicado pelas operações de HTTP History: agora
  `Request.metadata.id`, o ID numérico da linha exibido pelo Caido, é usado no
  lugar do `Request.id` interno do GraphQL.
- Alterado `caido_get_request` para localizar a linha pelo HTTPQL autoritativo
  `row.id.eq:<id>` e conferir que o ID retornado é exatamente o solicitado.
- Corrigido também o `requestId` publicado por folhas do Sitemap.
- Preservados seleção dinâmica de projeto, guardas antes/depois da leitura,
  paginação, HTTPQL, redaction e limites de body.
- Adicionados testes de regressão com IDs internos e visíveis diferentes,
  incluindo os casos observados `125 -> 119` e `119 -> 113`.

## v0.2.3

- Corrigida a divergência entre schema MCP publicado e validação dos handlers.
- Tornados explícitos os schemas de input/output derivados dos tipos Go usados
  pelos handlers.
- Alterado `caido_get_current_project` para retornar ID e nome em objeto plano.
- Adicionada validação de UUID canônico e existência do projeto.
- Mantidas as conferências do projeto antes e depois de cada leitura.
- Removida `caido_select_project`; a superfície voltou a ser 100% read-only.
- Adicionados testes MCP end-to-end para `tools/list`, `tools/call`, schemas,
  projeto ausente, UUID inválido, projeto inexistente e isolamento cruzado.

## v0.2.2

- Adicionadas `caido_get_current_project` e `caido_list_projects`.
- Adicionada `caido_select_project`, restrita a troca explícita do contexto
  local e validada por ID e nome exatos.
- Tornado `projectId` obrigatório em todas as cinco tools de dados.
- Adicionada validação do projeto antes e depois de cada leitura para bloquear
  mismatch e descartar resultados se o projeto mudar durante a operação.
- Adicionada identidade do projeto a todos os resultados de dados.
- Atualizado `doctor` para exibir o projeto que o MCP realmente enxerga.
- Mantida a ausência de Replay, envio de tráfego e mutação de dados capturados.

## v0.2.1

- Adicionado bootstrap com Windows DPAPI e tarefa de logon para o Secure MCP
  Tunnel.

## v0.2.0

- Adicionado suporte ao Secure MCP Tunnel.

## v0.1.0

- Release local inicial com cinco tools read-only.
