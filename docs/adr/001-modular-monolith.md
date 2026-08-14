# ADR-001: Monólito modular

- Estado da decisão: Aceita
- Estado de implementação: Fundação e Etapa 1 verificadas; adapter PostgreSQL da Etapa 2A implementado
- Data: 2026-08-14

## Contexto

O produto começará pequeno, mas deverá manter limites de domínio claros, alta testabilidade e capacidade de evolução. Distribuição prematura aumentaria complexidade operacional e de consistência sem requisitos que a justifiquem.

## Decisão

O backend será um monólito modular: um processo e um artefato de implantação, com módulos de negócio coesos e dependências explícitas. Domínio e casos de uso permanecerão independentes de frameworks, HTTP, persistência, IA e integrações externas.

Limites serão criados conforme o domínio for compreendido. Não haverá abstrações, barramentos ou pacotes compartilhados genéricos por antecipação.

## Consequências

- Desenvolvimento, testes e operação começam simples.
- Transações locais futuras podem preservar consistência sem coordenação distribuída.
- O adapter PostgreSQL permanece na borda do módulo e implementa a porta da aplicação sem inverter dependências.
- Disciplina de dependências é necessária para impedir um monólito acoplado.
- Extração de serviço só será considerada com evidência de necessidade operacional ou organizacional.
