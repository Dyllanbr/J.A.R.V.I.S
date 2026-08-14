# ADR-002: Go no backend

- Estado da decisão: Aceita
- Estado de implementação: Go 1.26.6 implementado na fundação
- Data: 2026-08-14

## Contexto

O backend precisa ser legível, previsível, eficiente, fácil de testar e econômico em dependências.

## Decisão

Usar Go no backend, priorizando biblioteca padrão, código idiomático, `gofmt`, `go vet` e testes nativos. A versão 1.26.6 é registrada em `go.mod` e `.go-version`.

## Consequências

- Binário único e modelo de concorrência adequado a serviços HTTP.
- Ferramentas oficiais cobrem formatação, análise básica, testes e detector de corrida.
- Interfaces e abstrações deverão permanecer pequenas e orientadas pelo consumidor.
- Dependências externas só serão aceitas com responsabilidade e benefício claros.
