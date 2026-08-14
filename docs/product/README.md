# Produto

O J.A.R.V.I.S. pretende ser um assessor financeiro pessoal. A Etapa 1 define o núcleo determinístico de uma despesa simples; a Etapa 2A persiste esse registro e um audit event mínimo em PostgreSQL. A Etapa 2B implementa uma boundary HTTP opt-in para preview, registro idempotente após confirmação do canal e consulta mensal. Ainda não existe UI, autenticação ou canal destinado a usuário real.

“Criar uma transaction” significa registrar no organizador uma despesa já ocorrida. J.A.R.V.I.S. não executa Pix, pagamento, compra, transferência, autorização bancária ou movimentação de fundos.

## Incluído agora

- `Money` exato em minor units, inicialmente BRL.
- `Expense` com valor positivo e vocabulário mínimo validado.
- `CreateExpense`, executado somente após confirmação explícita do canal futuro.
- Porta mínima de repositório e abstrações determinísticas de relógio e ID.
- Adapter PostgreSQL, migrations e atomicidade Expense + `EXPENSE_RECORDED`, aguardando revisão independente.

## Fora do escopo desta entrega

Confirmação por UI, receitas, parcelamentos, orçamento, metas, WhatsApp funcional, IA/OpenAI, MCP, agentes de produção, autenticação, banco de produção, nuvem e telas de aplicativo. Cada capacidade exigirá requisitos, modelagem, avaliação de segurança e critérios de aceitação próprios. A API atual não prova interação humana: o canal futuro deverá apresentar o preview e só então chamar o POST mutável.
