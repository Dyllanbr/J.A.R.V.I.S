# Playwright para API

Suíte TypeScript strict para o smoke da fundação e o E2E financeiro da Etapa 2B. O modo padrão verifica `GET /healthz` e método não permitido. O lifecycle de integração habilita também preview, criação/replay/conflito idempotente e consulta mensal contra API e PostgreSQL reais. Retries são zero.

Da raiz, o comando recomendado compila, inicia, aguarda e encerra exatamente o backend testado:

```bash
make bootstrap
make smoke
```

O lifecycle compartilhado recusa porta ocupada, usa timeouts de curl, acompanha o PID, imprime logs em falha e valida shutdown gracioso. `JARVIS_SMOKE_HOST` e `JARVIS_SMOKE_PORT` permitem alterar o endereço do harness. Como a suíte usa apenas o cliente HTTP do Playwright, binários de navegador não são instalados nesta fase.

O comando oficial financeiro é `make test-integration`. Ele cria banco e owner exclusivamente sintéticos, aplica migrations, inicia a API com `JARVIS_FINANCIAL_API_TESTS=true` e remove processo, container e volume. Não execute a suíte financeira contra dados pessoais.
