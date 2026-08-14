# ADR-005: Estratégia de testes por camada e risco

- Estado da decisão: Aceita
- Estado de implementação: Parcial; testes Go e smoke de API implementados, mobile e performance planejados
- Data: 2026-08-14

## Contexto

Confiabilidade requer feedback rápido no núcleo e verificação realista nos limites, sem criar suítes funcionais para capacidades inexistentes.

## Decisão

Usar testes nativos de Go para unidades e integração do backend, Playwright com TypeScript para APIs e Maestro para jornadas mobile futuras. Testes de performance serão definidos a partir de SLOs e cargas representativas.

O CI executará o mesmo `make verify` local: instalação limpa, whitespace, formatação, análise estática, testes com detector de corrida e cobertura, verificação de módulos, build, TypeScript, auditoria npm, OpenAPI semântico, scanner e smoke operacional sem retries.

## Consequências

- A maior parte da cobertura deverá permanecer rápida e próxima do código de domínio.
- Testes de ponta a ponta serão poucos e direcionados a riscos críticos.
- Dados de teste serão exclusivamente sintéticos.
- Ferramentas mobile e de performance não serão adicionadas antes de haver algo significativo para validar.
