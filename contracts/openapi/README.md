# OpenAPI

O contrato HTTP versionável do backend fica neste diretório. A especificação OpenAPI 3.1 está em `info.version: 0.4.0` e descreve o health check, os três endpoints financeiros compartilhados por despesas e receitas e a descoberta read-only do catálogo de categorias do sistema. `categoryId` é opcional em preview, create e itens do histórico; ausência significa “Sem categoria”, enquanto existência e applicability são validadas pelo catálogo. O modo padrão continua health-only; as rotas financeiras existem somente quando habilitadas explicitamente. Elas registram/consultam movimentações já ocorridas no organizador e nunca executam pagamento, recebimento, Pix, compra, transferência ou movimentação de fundos.

Valide semanticamente o documento OpenAPI 3.1 com a versão fixada do Redocly CLI:

```bash
make contract-check
```
