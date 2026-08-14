# ADR-007: API financeira idempotente e single-owner temporária

- Estado da decisão: Aceita
- Estado de implementação: Etapa 2B implementada; revisão independente pendente
- Data: 2026-08-14

## Contexto

O primeiro canal HTTP precisa validar uma despesa para revisão, registrá-la somente depois da confirmação do canal e listar o histórico mensal. Retries e concorrência não podem duplicar Expense ou AuditEvent. A porta `ExpenseRepository.Save` da etapa anterior não consegue incluir a metadata de idempotência na mesma transação sem ampliar uma responsabilidade específica.

Autenticação ainda não existe. Ao mesmo tempo, o cliente não pode escolher o owner, a origin ou a timezone financeira. A aplicação deve continuar health-only sem PostgreSQL quando o recurso financeiro não estiver habilitado.

## Decisão

`application.RecordExpense` consome uma porta pequena `ExpenseCommandStore`. O adapter PostgreSQL implementa essa porta com uma única DB transaction: reserva `(user_id, operation, idempotency_key)`, insere Expense, insere `EXPENSE_RECORDED`, conclui a reserva e faz commit. Conflitos são resolvidos pelo PostgreSQL, sem mutex ou cache em memória. Replay compara SHA-256 de uma representação canônica e carrega a Expense original do banco. A porta existente `ExpenseRepository` permanece para a capacidade já verificada; não foi criado UnitOfWork ou repository genérico.

O fingerprint inclui somente semântica normalizada: `EXPENSE`, descrição após trim permitido, minor units/BRL, forma de pagamento, instante UTC canônico, origin IOS e timezone `America/Sao_Paulo`. A camada de aplicação trunca instantes financeiros para microssegundos antes de preview, fingerprint, criação da entidade persistível e resposta. ID, Clock, timestamps de criação e bytes JSON não participam do fingerprint. `idempotency_records` armazena owner, operação, chave, hash, estado, referência da transaction e timestamps; não armazena descrição, valor, body ou response.

`PreviewExpense` reutiliza a validação do domínio e a canonicalização temporal compartilhada da aplicação sem ID nem qualquer porta de persistência. `ListExpensesByMonth` consome uma porta de leitura única, calcula limites IANA locais e consulta `[start,end)` em UTC, com filtro de owner/timezone e ordem `occurred_at DESC, id DESC`.

A composição financeira é opt-in por `JARVIS_FINANCIAL_API_ENABLED=true`. `JARVIS_OWNER_ID` é um contexto single-owner temporário derivado pelo servidor, não autenticação. O adapter HTTP atribui origin IOS e timezone `America/Sao_Paulo` e não aceita esses campos nem `userId` do cliente. O POST mutável representa registro no organizador após confirmação do canal; não executa pagamentos, Pix, compras, transferências ou movimentação de fundos.

## Consequências

- Migration 002 adiciona `idempotency_records` e o índice mensal sem alterar a migration 001 publicada.
- Primeira criação, audit event e metadata idempotente são atômicos; rollback não deixa chave envenenada.
- Replays entre instâncias futuras continuam corretos porque PostgreSQL é a fonte de verdade.
- Preview, criação inicial e replay representam `occurredAt` em UTC com precisão máxima de microssegundos. O Clock também é canonicalizado antes de produzir `createdAt` e `updatedAt`, de modo que a primeira resposta nunca seja mais precisa que o recurso persistido.
- Um erro de commit nunca produz resposta de sucesso. Outcomes indeterminados exigem retry com a mesma chave e tratamento operacional antes de uso real.
- Retenção da metadata, autenticação, autorização multiusuário e rate limiting distribuído continuam planejados.
- O modo padrão não abre pool, não exige `DATABASE_URL` e expõe somente `/healthz`.
