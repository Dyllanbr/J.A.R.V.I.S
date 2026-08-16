# Estratégia de testes

## Pirâmide inicial

1. Testes unitários nativos de Go para configuração, domínio e casos de uso atuais e futuros.
2. Testes de integração em limites reais quando adaptadores forem introduzidos.
3. Playwright com TypeScript para contrato e smoke tests HTTP.
4. XCTest/XCUITest para lógica, integração de cliente e primeira jornada iOS; Maestro para jornadas futuras quando as telas estabilizarem.
5. Testes de performance guiados por metas mensuráveis quando existirem cargas representativas.

## Implementado

- Testes Go para configuração, limites HTTP, health, 405, bind, cancelamento e shutdown gracioso.
- Testes unitários do domínio e aplicação para `Money`, `Expense`, `Income`, `CategoryID`, definitions/applicability, preview, record, fingerprints e projeção mensal; a semântica idempotente categorizada também possui cobertura direta, e fuzz permanece focado nas invariantes em que agrega valor.
- Testes de integração contra PostgreSQL 18.6 real para migrations 001–004, seed/FK composta de Category, matriz de tipos/payment method/audit/operação, ownership, limites Unicode/whitespace, rollback, concorrência, restart, DOWN seguro sob lock e consulta mensal mista.
- Testes de reserva/replay/conflito idempotente para `CREATE_EXPENSE` e `CREATE_INCOME`, incluindo payload igual/diferente concorrente, retry após falhas de transaction/audit/completion/commit, escopo por owner e defesa cross-type.
- Regressão HTTP/PostgreSQL entre instâncias independentes para replay após restart, precisão sub-microssegundo, body persistido estável e contagens `1 transaction + 1 AuditEvent + 1 idempotency record` por tipo.
- Prova durável do round trip `TIMESTAMPTZ` via pgx e testes unitários da canonicalização UTC/microssegundos usada por preview, fingerprint e criação.
- Lifecycle Docker compartilhado com porta efêmera, banco por teste e cleanup de container/volume em sucesso ou falha.
- Smoke Playwright health-only e E2E financeiro black-box contra API/PostgreSQL reais para catálogo, preview, create/replay/conflict de Expense/Income categorizados e histórico misto, com zero retries.
- Lifecycle compartilhado que valida porta, readiness do PID atual, cleanup e shutdown gracioso.
- Formatação, vet, lint TypeScript sem warnings, type-check, testes com detector de corrida, OpenAPI semântico, auditoria, scanner e build no CI.
- XCTest para Money input, codecs e modelos discriminados, cliente HTTP, cancelamento, catálogo compartilhado, máquina de estados/retry, stale Preview e histórico misto; a regressão de cancellation usa barreira positiva determinística entre waiters. XCUITest com stub `DEBUG` explícito cobre Expense/Income categorizados, picker/filtros de Category, alternância repetida das tabs por identifiers semânticos, History normal/erro/misto e Dynamic Type extremo.
- Gate local fail-closed para os dois fluxos `Simulator → SwiftUI → URLSession → Go → PostgreSQL`, com fixtures sintéticas únicas e pós-condição direta de `1 transaction + 1 AuditEvent + 1 idempotency record` por tipo e `category_id` esperado; Income também exige `payment_method IS NULL`.
- Job macOS independente para `make verify-ios`; o `make verify` cross-platform permanece inalterado.

O Incremento 1 — Despesas, incluindo o primeiro fluxo iOS, está **VERIFICADO**. O Incremento 2 — Receitas, incluindo domínio/aplicação, migration 003, PostgreSQL, API/OpenAPI, Playwright, iOS e E2E real, está **IMPLEMENTADO** e pronto para auditoria global independente; ainda não está classificado como verificado.

O Incremento 3A — Categorias e filtros do histórico está **IMPLEMENTADO** e aguarda auditoria final independente; ainda não está classificado como verificado.

## Ainda planejado

Não há fluxos Maestro, dados reais de produto, smoke em iPhone físico ou cenários de carga. Autenticação, rate limiting distribuído, armazenamento local seguro e recuperação de retry após restart continuam planejados.

Testes devem ser determinísticos, independentes e usar dados sintéticos. Flakiness deve ser tratada como defeito e não pode terminar verde por retry. `make verify` é o gate cross-platform; `make verify-ios` é o gate macOS/Simulator em modo stub; `make test-ios-integration` é a prova local ponta a ponta com cliente real e PostgreSQL. O modo real não possui fallback para stub e a URL chega ao app por configuração explícita do test bundle/launch environment.
