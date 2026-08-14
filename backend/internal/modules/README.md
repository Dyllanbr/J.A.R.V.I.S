# Módulos

Este diretório contém módulos de negócio criados a partir de requisitos aprovados. O primeiro é [`transactions`](transactions/README.md), restrito na Etapa 1 ao domínio de uma despesa simples e ao caso de uso `CreateExpense`.

Cada módulo encapsula seu domínio e casos de uso. Dependências de HTTP, banco de dados e integrações são adaptadores externos ao núcleo do módulo. Dependências entre módulos precisam ser explícitas e justificadas.
