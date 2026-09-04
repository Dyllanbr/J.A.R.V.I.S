# Backend

Backend Go em monólito modular. O processo permanece health-only por padrão; quando habilitado explicitamente, compõe os endpoints financeiros para despesas e receitas com PostgreSQL e um owner temporário derivado pelo servidor.

## Pacotes

- `cmd/api`: ponto de entrada e tratamento do ciclo de vida do processo.
- `internal/app`: composição e execução da aplicação.
- `internal/config`: configuração explícita por ambiente.
- `internal/platform/httpserver`: adaptador HTTP e limites do servidor.
- `internal/modules/transactions/domain`: `Money`, `CategoryID` opcional e os agregados separados `Expense`/`Income`, `CreditCard` e `InstallmentPlan`, sem infraestrutura.
- `internal/modules/transactions/application`: catálogo de categorias, preview, registro confirmado/idempotente, CardPurchase, cancelamento de InstallmentPlan e projeção mensal mista com portas consumidoras mínimas.
- `internal/modules/transactions/adapters/httpapi`: DTOs, decoding estrito e mapeamento HTTP fino.
- `internal/modules/transactions/adapters/postgres`: catálogo read-only, persistência Expense/Income/CreditCard/InstallmentPlan, command stores idempotentes e readers mensais.
- `internal/modules/transactions/adapters/randomid`: geração criptográfica de IDs opacos de Expense, Income, CreditCard e InstallmentPlan.
- `internal/platform/postgres`: configuração, pool e migrations fora do domínio.
- `cmd/migrate`: comando explícito para aplicar ou reverter migrations.

O Incremento 1 — Despesas, o Incremento 2 — Receitas e os Incrementos 3A, 3B e 3C estão **VERIFICADOS**. As subcapacidades 4A — CreditCard e 4B — CardPurchase + InstallmentPlan também estão **VERIFICADAS**, após auditoria independente e CI completo; o merge do 4B ocorreu no PR #75, commit `526cc855`.

O Incremento 4B acrescenta compra à vista ou parcelada vinculada a CreditCard, Expense total, InstallmentPlan, schedule derivado, preview/review/confirm, listagem/detalhe, cancellation preview/cancelamento, replay e idempotência. A persistência correspondente está na migration 008; parcelas futuras não são inseridas como novas Expenses e nenhuma operação movimenta dinheiro.

O módulo Go usa o caminho local `jarvis/backend` enquanto o repositório não possui URL canônica. Uma URL de módulo pública deve ser decidida antes da primeira publicação externa.

## Configuração operacional

- `JARVIS_HTTP_ADDRESS`: padrão `127.0.0.1:8080`; portas de 1 a 65535. Porta `0` é reservada a harnesses controlados de teste.
- `JARVIS_SHUTDOWN_TIMEOUT`: padrão `10s`, maior que zero e no máximo `30s`.
- `JARVIS_FINANCIAL_API_ENABLED`: `false`/ausente mantém health-only; `true` habilita a composição financeira.
- `JARVIS_OWNER_ID`: obrigatório e validado quando a API financeira está habilitada; é contexto single-owner temporário, não autenticação.

Configuração carregada somente por comandos/adapters PostgreSQL explícitos:

- `JARVIS_DATABASE_URL`: obrigatória para migrations; nunca é registrada em logs;
- `JARVIS_DB_MAX_CONNS`: padrão `4`, intervalo de 1 a 100;
- `JARVIS_DB_MIN_CONNS`: padrão `0`, de 0 até o máximo configurado;
- `JARVIS_DB_CONNECT_TIMEOUT`: padrão `5s`, máximo `30s`;
- `JARVIS_DB_OPERATION_TIMEOUT`: padrão `5s`, máximo `30s`.

Valores inválidos geram erros categóricos sem repetir o conteúdo bruto. O modo health-only não carrega configuração PostgreSQL; quando o modo financeiro está habilitado, o pool é obrigatório, usado pelos adapters e fechado no shutdown.

O servidor define timeouts para headers, leitura, escrita e conexões ociosas. O primeiro `SIGINT`/`SIGTERM` inicia shutdown gracioso; um segundo sinal volta ao comportamento padrão. O endpoint `GET /healthz` é exclusivamente operacional e não expõe dependências internas.

## Persistência local

Da raiz, após exportar as variáveis descritas no README principal:

```bash
make db-up
make migrate-up
make test-integration
make db-down
```

Os command stores usam queries parametrizadas e uma única DB transaction para reserva/conclusão idempotente, recursos financeiros, planos e eventos de auditoria. As operações `CREATE_EXPENSE`, `CREATE_INCOME` e `CREATE_CARD_PURCHASE`, junto dos eventos `EXPENSE_RECORDED`, `INCOME_RECORDED`, `CREDIT_CARD_CREATED`, `CREDIT_CARD_ARCHIVED`, `INSTALLMENT_PLAN_CREATED` e `INSTALLMENT_PLAN_CANCELLED`, permanecem coerentes com os agregados por constraints do PostgreSQL. Replay carrega snapshots históricos, e conflito de fingerprint não grava. Migrations não inserem fixtures. Os testes criam bancos descartáveis por caso; o lifecycle E2E migra, cria owners sintéticos, executa a API/Playwright e remove processo, container, rede e volume mesmo em falha.

`make migrate-down` reverte uma migration por chamada. O DOWN da migration 003 retorna ao schema anterior quando não existem Income rows; se existir qualquer Income persistida, falha atomicamente sem apagar ou converter dados.

A migration 004 cria o catálogo global de categorias do sistema e `transactions.category_id` nullable. Uma FK composta entre tipo e Category impede combinações incompatíveis no banco. O adapter de catálogo é somente leitura; ausência de Category permanece `NULL`, sem conversão para “Outros”. O DOWN obtém lock exclusivo, executa o guard sob esse lock e recusa atomicamente remover a infraestrutura enquanto existir transaction categorizada.

A migration 007 cria a persistência owner-scoped de CreditCard, com auditoria e idempotência próprias; `ACTIVE → ARCHIVED` é o lifecycle implementado. A migration 008 cria a persistência de InstallmentPlan, seus eventos/idempotência e os registros de idempotência de CardPurchase. A compra à vista registra somente a Expense total; a compra parcelada registra a Expense total e um plano. O cancelamento do plano não altera a Expense e o schedule continua sendo uma projeção derivada.
