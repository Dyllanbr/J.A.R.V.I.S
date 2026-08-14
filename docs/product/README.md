# Produto

O J.A.R.V.I.S. pretende ser um assessor financeiro pessoal. As Etapas 1, 2A e 2B verificaram o núcleo, a persistência/auditoria atômica e a boundary HTTP idempotente. A Etapa 3 implementa o primeiro canal SwiftUI: entrada, preview, revisão, confirmação explícita e histórico mensal. A UI aguarda revisão independente e continua destinada somente a desenvolvimento/testes sintéticos, sem autenticação ou usuário real.

“Criar uma transaction” significa registrar no organizador uma despesa já ocorrida. J.A.R.V.I.S. não executa Pix, pagamento, compra, transferência, autorização bancária ou movimentação de fundos.

## Incluído agora

- `Money` exato em minor units, inicialmente BRL.
- `Expense` com valor positivo e vocabulário mínimo validado.
- `CreateExpense`, executado somente após confirmação explícita do canal chamador.
- Porta mínima de repositório e abstrações determinísticas de relógio e ID.
- Adapter PostgreSQL, migrations, idempotência e atomicidade Expense + `EXPENSE_RECORDED`, verificados.
- Fluxo iOS `Registrar → preview → revisão → confirmação → sucesso → histórico`, implementado.

## Fora do escopo desta entrega

Receitas, parcelamentos, orçamento, metas, WhatsApp funcional, IA/OpenAI, MCP, agentes de produção, autenticação, armazenamento local seguro, banco de produção, nuvem e telas além desta jornada. Cada capacidade exigirá requisitos, modelagem, avaliação de segurança e critérios de aceitação próprios. O app atual sempre apresenta o preview antes de habilitar a confirmação que chama o POST mutável.
