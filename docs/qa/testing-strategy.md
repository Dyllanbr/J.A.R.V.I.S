# Estratégia de testes

## Pirâmide inicial

1. Testes unitários nativos de Go para configuração, domínio e casos de uso atuais e futuros.
2. Testes de integração em limites reais quando adaptadores forem introduzidos.
3. Playwright com TypeScript para contrato e smoke tests HTTP.
4. XCTest/XCUITest para lógica, integração de cliente e primeira jornada iOS; Maestro para jornadas futuras quando as telas estabilizarem.
5. Testes de performance guiados por metas mensuráveis quando existirem cargas representativas.

## Implementado

- Testes Go para configuração, limites HTTP, health, 405, bind, cancelamento e shutdown gracioso.
- Testes unitários do domínio e aplicação para `Money`, `Expense`, `Income`, `CategoryID`, `Recurrence`, `RecurrenceSuggestion`, civil date, detector determinístico, identidade da evidência, exclusions `ACTIVE`/`CANCELLED`, preview, record, cancelamento, fingerprints e projeção mensal; as semânticas idempotentes também possuem cobertura direta, e fuzz permanece focado nas invariantes em que agrega valor.
- Testes de integração contra PostgreSQL 18.6 real para migrations 001–006, seed/FK composta de Category, persistência dedicada de Recurrence, suppression de suggestions, matriz de tipos/payment method/audit/operação, ownership, limites Unicode/whitespace, corrupção semântica, rollback, concorrência, restart, `DateStyle` independente, replay histórico e DOWN seguro sob lock/guard.
- Testes de reserva/replay/conflito idempotente para `CREATE_EXPENSE`, `CREATE_INCOME`, `CREATE_RECURRENCE` e `CANCEL_RECURRENCE`, incluindo payload igual/diferente concorrente, retry após falhas, escopo por owner, defesa cross-type e replay histórico da criação mesmo após cancelamento posterior.
- Regressão HTTP/PostgreSQL entre instâncias independentes para replay após restart, precisão sub-microssegundo, body persistido estável e contagens `1 transaction + 1 AuditEvent + 1 idempotency record` por tipo.
- Prova durável do round trip `TIMESTAMPTZ` via pgx e testes unitários da canonicalização UTC/microssegundos usada por preview, fingerprint e criação.
- Lifecycle Docker compartilhado com porta efêmera, banco por teste e cleanup de container/volume em sucesso ou falha.
- Smoke Playwright health-only e E2E financeiro black-box contra API/PostgreSQL reais para catálogo, preview, create/replay/conflict de Expense/Income categorizados, histórico misto, preview/create/list/cancel de Recurrence e list/preview/dismiss/replay de suggestions com evidência materialmente nova, sempre sem retries para mascarar flakiness.
- Lifecycle compartilhado que valida porta, readiness do PID atual, cleanup e shutdown gracioso.
- Formatação, vet, lint TypeScript sem warnings, type-check, testes com detector de corrida, OpenAPI semântico, auditoria, scanner e build no CI.
- XCTest para Money input, codecs, data civil, modelos discriminados, Recurrence e RecurrenceSuggestion, cliente HTTP, cancelamento, catálogo compartilhado, máquinas de estados/retry, stale Preview, histórico misto e reconciliação concorrente de Recurrences/suggestions. As regressões de concorrência usam `CheckedContinuation`/barreiras determinísticas, sem sleeps ou polling. XCUITest com stub `DEBUG` explícito cobre Expense/Income categorizados, picker/filtros de Category, alternância repetida das três tabs por identifiers semânticos, History normal/erro/misto, Recurrences create/list/cancel, suggestions com Review/Confirm ou “Agora não”, acessibilidade e Dynamic Type extremo.
- Gate local fail-closed para os fluxos reais `Simulator → SwiftUI → URLSession → Go → PostgreSQL`, com fixtures sintéticas únicas e pós-condições diretas no banco. Expense/Income preservam `1 transaction + 1 AuditEvent + 1 idempotency record` por tipo e `category_id` esperado; Income também exige `payment_method IS NULL`. Recurrence possui persistência, audit event e idempotency record próprios e não produz writes nas tabelas legadas de transações. O fluxo real de suggestion prova três Expenses de evidência, zero Recurrence antes do `Confirm`, exatamente uma Recurrence após a confirmação e zero suppression nesse caminho.
- Job macOS independente para `make verify-ios`; o `make verify` cross-platform permanece inalterado.

O Incremento 1 — Despesas, incluindo o primeiro fluxo iOS, está **VERIFICADO**.

O Incremento 2 — Receitas, incluindo domínio/aplicação, migration 003, PostgreSQL, API/OpenAPI, Playwright, iOS e E2E real, está **VERIFICADO** após auditoria global independente e quality gates aplicáveis.

O Incremento 3A — Categorias e filtros do histórico está **VERIFICADO**. A auditoria final foi concluída com P0=0, P1=0 e P2=0. Os findings específicos de 3A, incluindo a corrida no DOWN da migration 004 e o cancelamento concorrente do catálogo compartilhado no iOS, foram resolvidos antes do merge.

O Incremento 3B — Recorrências confirmadas e assinaturas está **VERIFICADO**. A auditoria independente final aprovou o incremento com P0=0, P1=0, P2=0 e nenhum P3 novo de 3B. Os findings históricos do próprio incremento foram resolvidos, incluindo validação estrutural de `Recurrence`, replay antes de ID/Clock, independência de `DateStyle`, gaps de testes PostgreSQL, validação estrutural OpenAPI, schemas de erro fechados e as duas corridas de reconciliação/listagem no iOS.

Débitos técnicos históricos carregados e fora do escopo desses incrementos permanecem explícitos:

- Go `encoding/json` aceita nomes de propriedades com casing diferente de forma case-insensitive; dívida P3 carregada.
- `RecordIncome` ainda consome ID/Clock antes da detecção de replay; dívida P3 carregada, sem impacto de integridade conhecido.

O Incremento 3C — Detecção e sugestão de recorrências está **Verified**. A auditoria final independente foi concluída com P0=0, P1=0, P2=0 e P3=0. A verificação cobre detector Domain/Application, identidade determinística, exclusions `ACTIVE`/`CANCELLED`, suppression/idempotência, concorrência PostgreSQL, migration 006 e safe-DOWN, parity HTTP/OpenAPI, métodos estritos incluindo `HEAD`, Playwright, modelos/API/UI iOS, concorrência de UI, acessibilidade, segurança Debug/Release e E2E real com pós-condições SQL exatas.

## Evidências de verificação

As evidências de entrega e integração dos incrementos financeiros verificados incluem:

- **Incremento 2 — Receitas:** PR #67, merge commit `eca2b93`; implementação submetida à auditoria global independente e aos quality gates aplicáveis antes da classificação como **VERIFICADO**.
- **Incremento 3A — Categorias e filtros do histórico:** PR #68, merge commit `d9b066a`; auditoria final concluída com P0=0, P1=0 e P2=0. Os findings específicos de 3A foram resolvidos antes do merge; permanece apenas dívida P3 histórica carregada e não introduzida pelo incremento.
- **Incremento 3B — Recorrências confirmadas e assinaturas:** PR #69, merge commit `f557880`; auditoria independente final com P0=0, P1=0, P2=0 e nenhum P3 novo de 3B. Todos os findings históricos específicos do incremento foram resolvidos antes do merge. CI do PR concluiu com sucesso para `Build, lint and test` e `iOS build and tests`.
- **Incremento 3C — Detecção e sugestão de recorrências:** PR #72, merge commit `7268be4f9374bb7d2105cfa3aa9a58690cd34a85`; Issue #70 encerrada, Project status **Concluído** e auditoria final independente com P0=0, P1=0, P2=0 e P3=0. Os gates incluíram validação cross-layer, PostgreSQL real, contrato estrutural OpenAPI, Playwright, XCTest/XCUITest, segurança Release e integração Simulator → SwiftUI → URLSession → Go → PostgreSQL.

Esses registros comprovam integração na `main`, mas não substituem as suítes, auditorias e demais gates descritos nesta estratégia.

## Ainda planejado

Não há fluxos Maestro, dados reais de produto, smoke em iPhone físico ou cenários de carga. Autenticação, rate limiting distribuído, armazenamento local seguro e recuperação de retry após restart continuam planejados.

Testes devem ser determinísticos, independentes e usar dados sintéticos. Flakiness deve ser tratada como defeito e não pode terminar verde por retry. `make verify` é o gate cross-platform; `make verify-ios` é o gate macOS/Simulator em modo stub; `make test-ios-integration` é a prova local ponta a ponta com cliente real e PostgreSQL. O modo real não possui fallback para stub e a URL chega ao app por configuração explícita do test bundle/launch environment.
