# Produto

O J.A.R.V.I.S. pretende ser um assessor financeiro pessoal. A Etapa 1 do Incremento 1 define somente o núcleo determinístico de registro de uma despesa simples; ainda não existe jornada, canal ou tela de produto.

## Incluído agora

- `Money` exato em minor units, inicialmente BRL.
- `Expense` com valor positivo e vocabulário mínimo validado.
- `CreateExpense`, executado somente após confirmação explícita do canal futuro.
- Porta mínima de repositório e abstrações determinísticas de relógio e ID.

## Fora do escopo desta entrega

Persistência de despesas, endpoints financeiros, confirmação por UI, idempotência, `AuditEvent`, receitas, parcelamentos, orçamento, metas, WhatsApp funcional, IA/OpenAI, MCP, agentes de produção, autenticação, credenciais locais, banco de dados funcional, nuvem e telas de aplicativo. Cada capacidade exigirá requisitos, modelagem, avaliação de segurança e critérios de aceitação próprios.
