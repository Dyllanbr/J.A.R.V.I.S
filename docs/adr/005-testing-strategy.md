# ADR-005: Estratégia de testes por camada e risco

- Estado da decisão: Aceita
- Estado de implementação: Parcial; testes Go, PostgreSQL, Playwright, XCTest/XCUITest e integração iOS implementados; Maestro e performance planejados
- Data: 2026-08-14

## Contexto

Confiabilidade requer feedback rápido no núcleo e verificação realista nos limites, sem criar suítes funcionais para capacidades inexistentes.

## Decisão

Usar testes nativos de Go para unidades e integração do backend, Playwright com TypeScript para APIs e XCTest/XCUITest para o cliente iOS. Maestro permanece planejado para jornadas mobile estáveis futuras. Testes de performance serão definidos a partir de SLOs e cargas representativas.

O CI executará o mesmo `make verify` local: instalação limpa, whitespace, formatação, análise estática, testes com detector de corrida e cobertura, integração em PostgreSQL 18.6 real, migrations, verificação de módulos, build, TypeScript, auditoria npm, OpenAPI semântico, scanner e smoke operacional sem retries.

O job macOS separado executa `make verify-ios` com build, análise, XCTest e XCUITest/stub explícito. A prova local `make test-ios-integration` reutiliza o lifecycle PostgreSQL/API, passa pelo app e cliente URLSession reais no Simulator e exige pós-condição no PostgreSQL. O modo real falha fechado e não é mascarado por stub ou curl; Docker não é exigido no runner macOS.

## Consequências

- A maior parte da cobertura deverá permanecer rápida e próxima do código de domínio.
- Testes de ponta a ponta serão poucos e direcionados a riscos críticos.
- Dados de teste serão exclusivamente sintéticos.
- Integração PostgreSQL usa projeto Compose, banco e volume descartáveis, com cleanup obrigatório inclusive em falha.
- Ferramentas mobile e de performance não serão adicionadas antes de haver algo significativo para validar.
