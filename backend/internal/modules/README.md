# Módulos

Este diretório contém módulos de negócio criados a partir de requisitos aprovados. O primeiro é [`transactions`](transactions/README.md): núcleo de uma despesa simples, persistência atômica e boundaries idempotentes/HTTP estritamente limitadas à Etapa 2B.

Cada módulo encapsula seu domínio e casos de uso. Dependências de HTTP, banco de dados e integrações são adaptadores externos ao núcleo do módulo. Dependências entre módulos precisam ser explícitas e justificadas.
