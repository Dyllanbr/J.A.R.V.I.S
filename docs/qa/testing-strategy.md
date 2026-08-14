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
- Lifecycle Docker compartilhado com porta efêmera, banco por teste e cleanup de container/volume em sucesso ou falha.
- Smoke Playwright de health e método não permitido, com zero retries.
- Lifecycle compartilhado que valida porta, readiness do PID atual, cleanup e shutdown gracioso.
- Formatação, vet, lint TypeScript sem warnings, type-check, testes com detector de corrida, OpenAPI semântico, auditoria, scanner e build no CI.

`Money`, `Expense` e `CreateExpense` estão **VERIFICADOS** pela Etapa 1 mergeada. Migrations, adapter PostgreSQL e audit event atômico estão **IMPLEMENTADOS** e aguardam revisão independente da Etapa 2A.

## Ainda planejado

Não há testes funcionais de endpoint financeiro, fluxos Maestro, dados reais de produto ou cenários de carga, porque essas boundaries e jornadas ainda não existem. Idempotência permanece planejada para a Etapa 2B.

Testes futuros devem ser determinísticos, independentes e usar dados sintéticos. Flakiness deve ser tratada como defeito e não pode terminar verde por retry. O comando oficial de evidência completa é `make verify`.
