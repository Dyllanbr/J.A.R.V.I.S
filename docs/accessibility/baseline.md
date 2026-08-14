# Baseline de acessibilidade

## Estado

WCAG 2.2 nível AA é a baseline mínima **PLANEJADA** do J.A.R.V.I.S. Não existe alegação de conformidade: a fundação não possui UI, projeto Xcode ou componente funcional. Os critérios aplicáveis serão mapeados ao contexto iOS/SwiftUI antes da implementação.

## Requisitos para interfaces futuras

Os critérios de aceite deverão contemplar, conforme aplicável:

- VoiceOver e semântica nativa;
- labels, values, hints e traits precisos;
- agrupamento acessível e ordem de foco previsível;
- foco visível e não obscurecido;
- Dynamic Type sem perda de conteúdo ou operação;
- Reduce Motion e alternativas a movimento não essencial;
- contraste e uso de cor que não seja a única forma de comunicar estado;
- alternativas textuais para conteúdo não textual;
- alvos de toque mensuráveis e adequados aos critérios aplicáveis e à plataforma;
- autenticação acessível quando autenticação vier a existir.

Testes automatizados apoiam regressão, mas não substituem validação manual com VoiceOver, tamanhos extremos de texto, preferências de movimento e demais tecnologias assistivas. Regressões críticas de acessibilidade bloqueiam release.

## Rastreabilidade

A matriz [WCAG → requisito → componente → caso de teste → evidência](wcag-traceability-matrix.md) é o registro obrigatório. Ela começa como template vazio porque ainda não existem componentes funcionais. Nenhum requisito poderá ser classificado como VERIFICADO sem evidência associada.
