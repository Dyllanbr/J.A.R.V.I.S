# Backend

Backend Go em monólito modular. O processo HTTP continua restrito ao health check operacional; o núcleo financeiro da Etapa 1 não está conectado à composição, a transportes ou à persistência.

## Pacotes

- `cmd/api`: ponto de entrada e tratamento do ciclo de vida do processo.
- `internal/app`: composição e execução da aplicação.
- `internal/config`: configuração explícita por ambiente.
- `internal/platform/httpserver`: adaptador HTTP e limites do servidor.
- `internal/modules/transactions/domain`: `Money` e invariantes de uma despesa simples, sem infraestrutura.
- `internal/modules/transactions/application`: `CreateExpense` e suas portas consumidoras mínimas.

O módulo de transações está IMPLEMENTADO e aguarda revisão independente para receber estado VERIFICADO. Não existe adaptador de produção para suas portas.

O módulo Go usa o caminho local `jarvis/backend` enquanto o repositório não possui URL canônica. Uma URL de módulo pública deve ser decidida antes da primeira publicação externa.

## Configuração operacional

- `JARVIS_HTTP_ADDRESS`: padrão `127.0.0.1:8080`; portas de 1 a 65535. Porta `0` é reservada a harnesses controlados de teste.
- `JARVIS_SHUTDOWN_TIMEOUT`: padrão `10s`, maior que zero e no máximo `30s`.

O servidor define timeouts para headers, leitura, escrita e conexões ociosas. O primeiro `SIGINT`/`SIGTERM` inicia shutdown gracioso; um segundo sinal volta ao comportamento padrão. O endpoint `GET /healthz` é exclusivamente operacional e não expõe dependências internas.
