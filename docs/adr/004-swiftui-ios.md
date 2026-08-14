# ADR-004: SwiftUI para o aplicativo iOS

- Estado da decisão: Aceita
- Estado de implementação: Planejado; não existe projeto Xcode nem código Swift
- Data: 2026-08-14

## Contexto

O cliente inicial planejado é iOS e deverá tratar acessibilidade, desempenho e integração com a plataforma como requisitos de primeira classe.

## Decisão

Usar SwiftUI para o aplicativo iOS, adotando APIs nativas e arquitetura definida quando surgirem as primeiras jornadas. O contrato com o backend será explícito e versionado.

## Consequências

- O projeto iOS será criado somente com requisitos de tela aprovados.
- WCAG 2.2 nível AA é a baseline mínima planejada; VoiceOver, Dynamic Type, contraste, semântica, foco, alvos e redução de movimento entrarão nos critérios de aceite.
- Automação não substituirá validação manual com tecnologias assistivas, e regressões críticas bloquearão release.
- Maestro será usado para jornadas críticas depois que existirem telas estáveis.
- Autenticação, Face ID, passkeys e PIN permanecem fora do escopo e exigem decisões separadas.
