# ADR-003: PostgreSQL para persistência

- Estado da decisão: Aceita
- Estado de implementação: Etapa 2A implementada; revisão independente pendente
- Data: 2026-08-14

## Contexto

O núcleo de despesa verificado precisa de integridade referencial e gravação atômica com evidência mínima de auditoria. Persistência real também deve ser validada contra o mesmo mecanismo local e no CI sem acoplar domínio ou aplicação a um driver.

## Decisão

PostgreSQL 18.6 é a persistência relacional da Etapa 2A. O ambiente local/CI usa a imagem fixada por tag e digest. O adapter usa `pgx/v5` 5.10.0 e implementa a porta existente `ExpenseRepository`; domínio e aplicação não dependem do banco. Migrations SQL versionadas usam `tern/v2` 2.4.1 fora do domínio.

O schema inicial contém somente `users`, `transactions` e `audit_events`. `users` fornece ownership e integridade referencial sem autenticação ou dados cadastrais. `Save` grava a Expense e `EXPENSE_RECORDED` na mesma DB transaction. O audit event não replica descrição, valor nem payload financeiro. Idempotência não integra este schema e permanece planejada para a boundary de comando/API da Etapa 2B.

Instantes são `TIMESTAMPTZ`; o identificador IANA financeiro é persistido separadamente. Valores usam `BIGINT` em minor units. Constraints simples reforçam invariantes fundamentais sem usar enum nativo, ORM ou SQL dinâmico. A consistência de ownership do audit event é relacional: `(aggregate_id, user_id)` referencia a chave candidata `(id, user_id)` de `transactions`; a versão do aggregate não participa dessa FK nesta etapa.

Como defesa em profundidade, a migration enumera de forma explícita e independente de locale o conjunto Unicode White_Space usado por `strings.TrimSpace` no Go 1.26.6. Isso rejeita whitespace externo nos valores persistidos correspondentes e descrições vazias após essa regra, sem alterar whitespace interno ou normalizar Unicode. O domínio Go permanece a autoridade, e uma futura atualização da tabela Unicode da toolchain exige revisar o conjunto SQL. A API health-only não abre pool nem exige configuração PostgreSQL.

## Consequências

- O lifecycle local/CI depende de Docker e executa migrations/testes em bancos descartáveis; cleanup remove containers e volumes de teste.
- O volume de desenvolvimento é persistente por escolha explícita e pode ser removido com `docker compose down --volumes`.
- SQL ficará em adaptadores de persistência, nunca em handlers HTTP.
- O pool começa com limites e timeouts conservadores configuráveis; isso não é resultado de tuning de produção.
- Papéis de produção, TLS, backup, criptografia, retenção, fornecedores e acesso permanecem sem decisão até existir ambiente e uso real.
