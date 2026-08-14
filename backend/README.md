# Backend

Backend Go em monólito modular. O processo permanece health-only por padrão; quando habilitado explicitamente, compõe os endpoints financeiros da Etapa 2B com PostgreSQL e um owner temporário derivado pelo servidor.

## Pacotes

- `cmd/api`: ponto de entrada e tratamento do ciclo de vida do processo.
- `internal/app`: composição e execução da aplicação.
- `internal/config`: configuração explícita por ambiente.
- `internal/platform/httpserver`: adaptador HTTP e limites do servidor.
- `internal/modules/transactions/domain`: `Money` e invariantes de uma despesa simples, sem infraestrutura.
- `internal/modules/transactions/application`: preview, criação confirmada/idempotente, consulta mensal e portas consumidoras mínimas.
- `internal/modules/transactions/adapters/httpapi`: DTOs, decoding estrito e mapeamento HTTP fino.
- `internal/modules/transactions/adapters/postgres`: `ExpenseRepository`, command store idempotente e reader mensal.
- `internal/modules/transactions/adapters/randomid`: geração criptográfica de IDs opacos de Expense.
- `internal/platform/postgres`: configuração, pool e migrations fora do domínio.
- `cmd/migrate`: comando explícito para aplicar ou reverter migrations.

O núcleo `Money`/`Expense`/`CreateExpense`, a migration 001, o adapter base e a persistência de audit event estão **VERIFICADOS** pelas etapas anteriores. Idempotência, migration 002, preview, HTTP financeiro e query mensal estão **IMPLEMENTADOS** e aguardam revisão independente da Etapa 2B.

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

O command store usa queries parametrizadas e uma única DB transaction para reserva/conclusão idempotente, `transactions` e `audit_events`. Replay carrega o recurso original do banco, e conflito de fingerprint não grava. Migrations não inserem fixtures. Os testes criam bancos descartáveis por caso; o lifecycle E2E migra, cria um owner sintético, executa a API/Playwright e remove processo, container, rede e volume mesmo em falha.

`make migrate-down` reverte uma migration por chamada. Depois da migration 002, uma chamada preserva o schema publicado da migration 001.
