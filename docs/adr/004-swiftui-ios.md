# ADR-004: SwiftUI para o aplicativo iOS

- Estado da decisão: Aceita
- Estado de implementação: Incremento 1 verificado
- Data: 2026-08-14

## Contexto

O cliente inicial planejado é iOS e deverá tratar acessibilidade, desempenho e integração com a plataforma como requisitos de primeira classe.

## Decisão

Usar SwiftUI para o aplicativo iOS 17 com Swift concurrency, Foundation, URLSession e XCTest/XCUITest, sem dependências externas. A primeira jornada segue `View -> ViewModel -> FinancialAPI -> URLSession`; o OpenAPI versionado permanece a fonte do contrato.

## Consequências

- O projeto Xcode, o scheme compartilhado, as features de registro/histórico e os testes são versionados e reproduzíveis via `xcodebuild`.
- WCAG 2.2 nível AA é a baseline mínima planejada; VoiceOver, Dynamic Type, contraste, semântica, foco, alvos e redução de movimento entrarão nos critérios de aceite.
- Automação não substituirá validação manual com tecnologias assistivas, e regressões críticas bloquearão release.
- XCUITest cobre a primeira jornada com stub `DEBUG` explícito e identifiers semânticos; um gate local fail-closed comprova Simulator → app → URLSession → API → PostgreSQL real e valida a pós-condição no banco. Maestro permanece planejado até as telas estabilizarem.
- Networking local por ATS é permitido somente no Info.plist de Debug/integration; Release não possui essa exceção.
- Autenticação, Face ID, passkeys e PIN permanecem fora do escopo e exigem decisões separadas.
- Não há persistência financeira local; retry idempotente pendente existe somente em memória e recuperação após restart permanece planejada junto de auth/armazenamento seguro.
