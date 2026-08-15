# Módulos

Este diretório contém módulos de negócio criados a partir de requisitos aprovados. O primeiro é [`transactions`](transactions/README.md): agregados separados de despesa e receita, persistência/auditoria atômicas, boundaries idempotentes/HTTP e projeção mensal mista de leitura.

Cada módulo encapsula seu domínio e casos de uso. Dependências de HTTP, banco de dados e integrações são adaptadores externos ao núcleo do módulo. Dependências entre módulos precisam ser explícitas e justificadas.
