# Estratégia de testes

## Pirâmide inicial

1. Testes unitários nativos de Go para configuração, domínio e casos de uso atuais e futuros.
2. Testes de integração em limites reais quando adaptadores forem introduzidos.
3. Playwright com TypeScript para contrato e smoke tests HTTP.
4. Maestro para poucas jornadas mobile críticas quando existir aplicativo.
5. Testes de performance guiados por metas mensuráveis quando existirem cargas representativas.

## Implementado

- Testes Go para configuração, limites HTTP, health, 405, bind, cancelamento e shutdown gracioso.
- Testes unitários do domínio de transações e do caso de uso `CreateExpense`, incluindo seeds de fuzz para `Money` e descrições.
- Testes de integração contra PostgreSQL 18.6 real para migrations, tipos e constraints estruturais, ownership do audit event, limites Unicode/whitespace, adapter, rollback, unicidade, duplicidade e cancelamento.
- Testes PostgreSQL da migration 002, reserva/replay/conflito idempotente, concorrência de payload igual/diferente, rollback e retry após falhas de Expense/AuditEvent/commit, escopo por owner e query mensal.
- Regressão HTTP/PostgreSQL entre duas instâncias independentes para replay após restart, incluindo precisão sub-microssegundo, igualdade integral do recurso e contagens `1 Expense + 1 AuditEvent + 1 idempotency record`.
- Prova durável do round trip `TIMESTAMPTZ` via pgx e testes unitários da canonicalização UTC/microssegundos usada por preview, fingerprint e criação.
- Lifecycle Docker compartilhado com porta efêmera, banco por teste e cleanup de container/volume em sucesso ou falha.
- Smoke Playwright health-only e E2E financeiro contra API/PostgreSQL reais, com zero retries.
- Lifecycle compartilhado que valida porta, readiness do PID atual, cleanup e shutdown gracioso.
- Formatação, vet, lint TypeScript sem warnings, type-check, testes com detector de corrida, OpenAPI semântico, auditoria, scanner e build no CI.

`Money`, `Expense` e `CreateExpense` estão **VERIFICADOS** pela Etapa 1 mergeada. Migration 001, adapter PostgreSQL base e audit event atômico estão **VERIFICADOS** pela Etapa 2A. Idempotência, migration 002, preview, HTTP financeiro e query mensal estão **IMPLEMENTADOS** e aguardam revisão independente da Etapa 2B.

## Ainda planejado

Não há fluxos Maestro, dados reais de produto ou cenários de carga. Autenticação, rate limiting distribuído e testes de UI continuam planejados porque essas boundaries ainda não existem.

Testes futuros devem ser determinísticos, independentes e usar dados sintéticos. Flakiness deve ser tratada como defeito e não pode terminar verde por retry. O comando oficial de evidência completa é `make verify`.
