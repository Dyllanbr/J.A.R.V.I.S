# ADR-003: PostgreSQL para persistência futura

- Estado da decisão: Aceita
- Estado de implementação: Planejado; nenhum banco, driver, schema ou SQL existe
- Data: 2026-08-14

## Contexto

Dados financeiros futuros exigirão integridade, transações e consultas confiáveis. O núcleo de despesa já existe, mas ainda não há requisitos de persistência suficientes para desenhar schema ou transações de auditoria e idempotência.

## Decisão

PostgreSQL será a opção padrão quando persistência relacional for necessária. O domínio e os casos de uso não dependerão do banco; adaptadores implementarão contratos definidos por necessidades reais da aplicação.

## Consequências

- Modelagem relacional e migrações serão tratadas em tarefa futura.
- Nenhum driver, container ou recurso de nuvem é adicionado agora.
- SQL ficará em adaptadores de persistência, nunca em handlers HTTP.
- Backup, criptografia, retenção e acesso precisarão de decisões próprias antes de dados reais.
