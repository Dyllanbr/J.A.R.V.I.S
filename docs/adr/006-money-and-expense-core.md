# ADR-006: Representação monetária e núcleo de despesas

- Estado da decisão: Aceita
- Estado de implementação: Etapa 1 verificada
- Data: 2026-08-14

## Contexto

O primeiro núcleo financeiro precisa representar valores com exatidão, aplicar invariantes sem depender de canais ou persistência e permanecer determinístico em testes. Representações em ponto flutuante introduziriam arredondamento implícito, enquanto tipos ou frameworks financeiros genéricos antecipariam necessidades ainda inexistentes.

## Decisão

`Money` usa `int64` em minor units. Como BRL é a única moeda inicialmente aceita, a representação interna guarda somente o valor inteiro: seu zero value é R$ 0,00 BRL e `Currency()` sempre retorna BRL. O construtor continua recebendo a moeda solicitada e rejeita qualquer valor diferente de BRL. Não será antecipada uma representação interna para múltiplas moedas. O valor monetário genérico pode ser zero ou assinado; `Expense` exige valor estritamente positivo e carrega o significado `EXPENSE`, sem representar despesa por sinal negativo.

O módulo `transactions` separa `domain` de `application`. `Expense` possui estado inicial `RECORDED` e versão `1`; seu instante é normalizado para UTC e o timezone financeiro IANA permanece explícito. Seus identificadores opacos usam UTF-8 válido, têm limite de 128 bytes, rejeitam caracteres de controle e espaços externos, sem impor UUID. `CreateExpense` é executado somente depois da confirmação obtida pelo canal, valida todos os dados do canal pela única fonte de regras do domínio antes de consultar ID e relógio e depende das portas mínimas de repositório, relógio e geração de ID.

## Consequências

- R$ 42,50 é representado exatamente por `4250` e `BRL`; `float32` e `float64` não participam do domínio.
- O intervalo suportado nesta etapa é o de `int64`; operações aritméticas e políticas de overflow não são adicionadas sem caso de uso.
- A representação interna terá de ser reavaliada apenas se uma segunda moeda for realmente introduzida; essa flexibilidade não é antecipada agora.
- Canais futuros traduzirão suas entradas para os tipos do núcleo e não duplicarão regras financeiras.
- A validação de timezones IANA depende de tzdata no ambiente operacional. Antes de containerização ou deploy, a presença dessa base deverá ser verificada; `America/Sao_Paulo` permanece baseline e UTC-3 nunca será hard-coded.
- Idempotência foi implementada posteriormente na command/API boundary da Etapa 2B, conforme o ADR-007, sem entrar em `Money` ou `Expense`. A persistência atômica do audit event foi implementada pela Etapa 2A, conforme o ADR-003, sem alterar este núcleo.
- O domínio permanece sem SQL, PostgreSQL, endpoint ou UI.
