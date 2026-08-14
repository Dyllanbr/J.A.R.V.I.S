# Módulos

Este diretório receberá módulos de negócio quando houver requisitos aprovados. Ele permanece sem código nesta fundação para evitar abstrações e limites artificiais antes do domínio existir.

Cada módulo futuro deverá encapsular seu domínio e casos de uso. Dependências de HTTP, banco de dados e integrações serão adaptadores externos ao núcleo do módulo. Dependências entre módulos precisarão ser explícitas e justificadas.
