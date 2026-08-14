# Playwright para API

Suíte TypeScript strict destinada ao smoke da fundação. Ela verifica o comportamento observável de `GET /healthz` e a resposta genérica para método não permitido. Retries são zero.

Da raiz, o comando recomendado compila, inicia, aguarda e encerra exatamente o backend testado:

```bash
make bootstrap
make smoke
```

O lifecycle compartilhado recusa porta ocupada, usa timeouts de curl, acompanha o PID, imprime logs em falha e valida shutdown gracioso. `JARVIS_SMOKE_HOST` e `JARVIS_SMOKE_PORT` permitem alterar o endereço do harness. Como a suíte usa apenas o cliente HTTP do Playwright, binários de navegador não são instalados nesta fase.
