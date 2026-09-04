# Playwright para API

Suíte TypeScript strict para o smoke da fundação e o E2E financeiro. O modo padrão verifica `GET /healthz` e método não permitido. O lifecycle de integração habilita também preview, criação/replay/conflito idempotente de Expense e Income, catálogo de categorias, CreditCard, CardPurchase e InstallmentPlan contra API e PostgreSQL reais. Retries são zero.

Os cenários 4A/4B exercitam operações explícitas de CreditCard, compra à vista e parcelada, preview/review/confirm, replay, listagem/detalhe de InstallmentPlan, cancellation preview e cancelamento. Eles validam que a compra à vista cria uma Expense total sem plano, que a compra parcelada cria uma Expense total e um plano com schedule derivado e que nenhuma parcela futura vira uma nova Expense. O Playwright usa somente os endpoints reais e não substitui a evidência dos testes nativos iOS.

Da raiz, o comando recomendado compila, inicia, aguarda e encerra exatamente o backend testado:

```bash
make bootstrap
make smoke
```

O lifecycle compartilhado recusa porta ocupada, usa timeouts de curl, acompanha o PID, imprime logs em falha e valida shutdown gracioso. `JARVIS_SMOKE_HOST` e `JARVIS_SMOKE_PORT` permitem alterar o endereço do harness. Como a suíte usa apenas o cliente HTTP do Playwright, binários de navegador não são instalados nesta fase.

O comando oficial financeiro é `make test-integration`. Ele cria banco e owners exclusivamente sintéticos, aplica migrations 001–008, inicia a API com `JARVIS_FINANCIAL_API_TESTS=true` e remove processo, container e volume. Não execute a suíte financeira contra dados pessoais. Fluxos Maestro permanecem planejados e não são exercitados por esta suíte.
