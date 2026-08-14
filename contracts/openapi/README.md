# OpenAPI

O contrato HTTP versionável do backend fica neste diretório. A especificação descreve o health check e os três endpoints financeiros implementados na Etapa 2B. O modo padrão continua health-only; as rotas financeiras existem somente quando habilitadas explicitamente. Elas registram/consultam despesas no organizador e nunca executam pagamento, Pix, compra, transferência ou movimentação de fundos.

Valide semanticamente o documento OpenAPI 3.1 com a versão fixada do Redocly CLI:

```bash
make contract-check
```
