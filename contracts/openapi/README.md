# OpenAPI

O contrato HTTP versionável do backend fica neste diretório. A especificação OpenAPI 3.1 está em `info.version: 0.12.0` e descreve o health check, os endpoints de Expense/Income, categorias, Recurrence, RecurrenceSuggestion, CreditCard, CardPurchase/InstallmentPlan, SafeAvailable e MonthlyBudget. Os seis endpoints 4B são `/v1/card-purchases/preview`, `/v1/card-purchases`, `/v1/installment-plans`, `/v1/installment-plans/{installmentPlanId}`, `/v1/installment-plans/{installmentPlanId}/cancellation-preview` e `/v1/installment-plans/{installmentPlanId}/cancel`. O Incremento 5 acrescenta `GET /v1/safe-available` e `PUT/GET /v1/monthly-budgets/{month}`. `categoryId` permanece opcional nos fluxos financeiros em que é aplicável; ausência significa “Sem categoria”, enquanto existência e applicability são validadas pelo catálogo. O modo padrão continua health-only; as rotas financeiras existem somente quando habilitadas explicitamente. Elas registram/consultam movimentações já ocorridas, compromissos de parcelas confirmados e projeções read-only, mas nunca executam pagamento, recebimento, Pix, compra, transferência ou movimentação de fundos.

Valide semanticamente o documento OpenAPI 3.1 com a versão fixada do Redocly CLI:

```bash
make contract-check
```
