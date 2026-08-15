# OpenAPI

O contrato HTTP versionável do backend fica neste diretório. A especificação descreve o health check e os três endpoints financeiros compartilhados por despesas e receitas. O modo padrão continua health-only; as rotas financeiras existem somente quando habilitadas explicitamente. Elas registram/consultam movimentações já ocorridas no organizador e nunca executam pagamento, recebimento, Pix, compra, transferência ou movimentação de fundos.

Valide semanticamente o documento OpenAPI 3.1 com a versão fixada do Redocly CLI:

```bash
make contract-check
```
