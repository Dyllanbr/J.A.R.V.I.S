# Produto

O J.A.R.V.I.S. pretende ser um assessor financeiro pessoal. A Etapa 1 define o núcleo determinístico de uma despesa simples; a Etapa 2A persiste esse registro e um audit event mínimo em PostgreSQL. Ainda não existe jornada, canal, endpoint financeiro ou tela de produto.

## Incluído agora

- `Money` exato em minor units, inicialmente BRL.
- `Expense` com valor positivo e vocabulário mínimo validado.
- `CreateExpense`, executado somente após confirmação explícita do canal futuro.
- Porta mínima de repositório e abstrações determinísticas de relógio e ID.
- Adapter PostgreSQL, migrations e atomicidade Expense + `EXPENSE_RECORDED`, aguardando revisão independente.

## Fora do escopo desta entrega

Endpoints financeiros, confirmação por UI, idempotência, receitas, parcelamentos, orçamento, metas, WhatsApp funcional, IA/OpenAI, MCP, agentes de produção, autenticação, banco de produção, nuvem e telas de aplicativo. Cada capacidade exigirá requisitos, modelagem, avaliação de segurança e critérios de aceitação próprios.
