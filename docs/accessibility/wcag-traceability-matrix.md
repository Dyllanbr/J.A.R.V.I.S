# Matriz de rastreabilidade WCAG

Estado atual: primeira jornada iOS **IMPLEMENTADA**, aguardando revisão independente e evidência manual. WCAG 2.2 AA é a baseline mínima; a tabela não é alegação de conformidade.

| Critério WCAG | Requisito | Componente | Caso de teste | Evidência | Estado |
| --- | --- | --- | --- | --- | --- |
| 1.3.1 / 4.1.2 | Labels, valores e agrupamento semântico | Abas, formulário, revisão, sucesso e histórico | XCUITest exige identifiers semânticos em runtime, alterna as tabs repetidamente e valida History normal/erro sem copy traduzível | `make verify-ios`; VoiceOver manual pendente | IMPLEMENTADO |
| 1.4.3 / 1.4.11 | Contraste via cores/componentes do sistema | Todas as views | Revisão visual Light/Dark e contraste manual | Pendente de revisão independente | IMPLEMENTADO |
| 1.4.4 / 1.4.10 | Dynamic Type e reflow sem perda funcional | Formulário, revisão, sucesso e histórico | XCUITest em `UIContentSizeCategoryAccessibilityExtraExtraExtraLarge`; revisão visual manual | `make verify-ios`; manual pendente | IMPLEMENTADO |
| 2.1.1 / 2.4.3 | Operação e ordem de foco previsíveis | Entrada → revisão → confirmação | XCUITest da jornada; VoiceOver manual | `JARVISUITests`; manual pendente | IMPLEMENTADO |
| 2.4.11 | Foco não obscurecido | Campos e teclado do formulário | Form permite scroll e dismiss imediato do teclado; teste UI percorre formulário | XCUITest; manual pendente | IMPLEMENTADO |
| 2.5.8 | Alvos de toque adequados | Botões, navegação mensal e ações | Inspeção das áreas mínimas de 44 pt | Revisão independente pendente | IMPLEMENTADO |
| 3.3.1 / 3.3.2 | Erros e instruções identificáveis sem dados técnicos | Formulário e estados de rede | XCTest de mapeamento seguro; validação manual | `JARVISTests`; manual pendente | IMPLEMENTADO |
| 2.3.3 | Reduce Motion | Jornada completa | Ausência de animação customizada obrigatória | Inspeção de código; teste manual pendente | IMPLEMENTADO |
| 3.3.8 | Autenticação acessível | Autenticação futura | A definir com a feature | Nenhuma | PLANEJADO |

Testes automáticos e manuais são registrados separadamente. Automação não substitui VoiceOver, Dynamic Type extremo, contraste e demais validações com tecnologias assistivas; regressões críticas bloqueiam release.
