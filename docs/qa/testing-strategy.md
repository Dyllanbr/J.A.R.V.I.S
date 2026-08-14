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
- Smoke Playwright de health e método não permitido, com zero retries.
- Lifecycle compartilhado que valida porta, readiness do PID atual, cleanup e shutdown gracioso.
- Formatação, vet, lint TypeScript sem warnings, type-check, testes com detector de corrida, OpenAPI semântico, auditoria, scanner e build no CI.

`CreateExpense` está **IMPLEMENTADO**. Sua classificação como **VERIFICADO** permanece pendente da reauditoria independente desta rodada e das evidências exigidas pela Definition of Done.

## Ainda planejado

Não há testes funcionais de endpoint financeiro, fluxos Maestro, ambiente PostgreSQL, dados de produto ou cenários de carga, porque esses adaptadores e jornadas ainda não existem.

Testes futuros devem ser determinísticos, independentes e usar dados sintéticos. Flakiness deve ser tratada como defeito e não pode terminar verde por retry. O comando oficial de evidência completa é `make verify`.
