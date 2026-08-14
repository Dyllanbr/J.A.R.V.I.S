# Backend

Backend Go em monólito modular. O processo HTTP continua restrito ao health check operacional; a persistência PostgreSQL da Etapa 2A ainda não está conectada à composição HTTP.

## Pacotes

- `cmd/api`: ponto de entrada e tratamento do ciclo de vida do processo.
- `internal/app`: composição e execução da aplicação.
- `internal/config`: configuração explícita por ambiente.
- `internal/platform/httpserver`: adaptador HTTP e limites do servidor.
- `internal/modules/transactions/domain`: `Money` e invariantes de uma despesa simples, sem infraestrutura.
- `internal/modules/transactions/application`: `CreateExpense` e suas portas consumidoras mínimas.
- `internal/modules/transactions/adapters/postgres`: implementação PostgreSQL de `ExpenseRepository`.
- `internal/platform/postgres`: configuração, pool e migrations fora do domínio.
- `cmd/migrate`: comando explícito para aplicar ou reverter migrations.

O núcleo `Money`/`Expense`/`CreateExpense` está **VERIFICADO** pela Etapa 1. O adapter, as migrations e a persistência de audit event estão **IMPLEMENTADOS** e aguardam revisão independente da Etapa 2A. HTTP financeiro e idempotência continuam **PLANEJADOS**.

O módulo Go usa o caminho local `jarvis/backend` enquanto o repositório não possui URL canônica. Uma URL de módulo pública deve ser decidida antes da primeira publicação externa.

## Configuração operacional

- `JARVIS_HTTP_ADDRESS`: padrão `127.0.0.1:8080`; portas de 1 a 65535. Porta `0` é reservada a harnesses controlados de teste.
- `JARVIS_SHUTDOWN_TIMEOUT`: padrão `10s`, maior que zero e no máximo `30s`.

Configuração carregada somente por comandos/adapters PostgreSQL explícitos:

- `JARVIS_DATABASE_URL`: obrigatória para migrations; nunca é registrada em logs;
- `JARVIS_DB_MAX_CONNS`: padrão `4`, intervalo de 1 a 100;
- `JARVIS_DB_MIN_CONNS`: padrão `0`, de 0 até o máximo configurado;
- `JARVIS_DB_CONNECT_TIMEOUT`: padrão `5s`, máximo `30s`;
- `JARVIS_DB_OPERATION_TIMEOUT`: padrão `5s`, máximo `30s`.

Valores inválidos geram erros categóricos sem repetir o conteúdo bruto. A API atual não carrega essa configuração, portanto sua inicialização não depende do banco.

O servidor define timeouts para headers, leitura, escrita e conexões ociosas. O primeiro `SIGINT`/`SIGTERM` inicia shutdown gracioso; um segundo sinal volta ao comportamento padrão. O endpoint `GET /healthz` é exclusivamente operacional e não expõe dependências internas.

## Persistência local

Da raiz, após exportar as variáveis descritas no README principal:

```bash
make db-up
make migrate-up
make test-integration
make db-down
```

O adapter usa queries parametrizadas e uma única DB transaction para `transactions` e `audit_events`. Migrations não inserem fixtures. Os testes criam bancos descartáveis por caso e o script remove container, rede e volume mesmo em falha.
