# Aplicativo iOS

Estado: **IMPLEMENTADO na Etapa 3; revisão independente pendente**.

O primeiro cliente nativo do J.A.R.V.I.S. usa SwiftUI, Swift concurrency, Foundation e URLSession, sem dependências externas. O deployment target é iOS 17.0, o target/scheme compartilhado é `JARVIS` e o bundle identifier de desenvolvimento é `dev.jarvis.JARVIS`.

## Escopo atual

- tab bar UIKit nativa com conteúdo SwiftUI para **Registrar** e **Histórico**;
- formulário de despesa, preview obrigatório, revisão congelada e confirmação explícita;
- retry em memória com a mesma `Idempotency-Key` durante uma tentativa lógica;
- histórico mensal via `GET /v1/transactions?month=YYYY-MM`;
- cliente HTTP explícito com `URLSessionConfiguration.ephemeral`;
- testes XCTest e XCUITest com stub disponível apenas em `DEBUG` e selecionado explicitamente;
- caminho automatizado Simulator → app → URLSession → API real → PostgreSQL real, com pós-condição no banco.

O app somente registra uma despesa já ocorrida. Ele não executa Pix, pagamento, compra, transferência ou movimentação de fundos.

## Abrir e executar

Requisitos: macOS, Xcode com um runtime iOS compatível e um iPhone Simulator. Abra:

```bash
open apps/ios/JARVIS.xcodeproj
```

Selecione o scheme compartilhado `JARVIS` e um Simulator. Em `DEBUG`, sem override, o cliente usa `http://127.0.0.1:8080`. Para apontar a execução a outra API local, defina no launch environment:

```text
JARVIS_IOS_API_BASE_URL=http://127.0.0.1:18081
```

Release não possui fallback silencioso para localhost nem a exceção ATS de networking local usada no Debug. A URL não pode conter credenciais. Owner, origin e timezone financeira são definidos no backend e nunca configurados no app.

## Comandos oficiais

A partir da raiz do monorepo:

```bash
make build-ios
make test-ios
make verify-ios
make test-ios-integration
```

`make verify-ios` executa build, análise estática e XCTest/XCUITest com `JARVIS_IOS_API_MODE=stub`. O script exige Xcode 16+, considera somente runtimes iOS 17+, prefere iPhone 15 e, na ausência dele, escolhe deterministicamente outro iPhone no runtime mais recente. Device, runtime e UDID são informados. Um Simulator que já estava ligado é preservado; um Simulator iniciado pelo script é desligado e aguardado no cleanup. Resultados/DerivedData ficam fora do repositório.

`make test-ios-integration` injeta `JARVIS_IOS_API_MODE=real`, a base URL e uma descrição sintética única no bundle do XCUITest; o test runner repassa esses valores ao processo do app. O modo real falha fechado se a URL estiver ausente, inválida ou indisponível e nunca usa o stub. O gate comprova no app preview → revisão → confirmação → sucesso → histórico e, ao final, consulta o PostgreSQL para exigir exatamente uma Expense, um `EXPENSE_RECORDED` e um registro idempotente concluído para a fixture. Ele exige Docker e limpa API, Simulator iniciado pelo script, container e volume mesmo em falha.

Ações e navegação do XCUITest usam identifiers semânticos (`tab.*`, `register.*`, `review.*` e `history.*`), não textos traduzíveis. `RootView` usa um `UITabBarController` nativo com um `UIHostingController` por tab; cada hosting controller recebe diretamente seu próprio `UITabBarItem` e identifier. Não há barra customizada nem associação por posição, texto, símbolo ou espera temporal. `bash scripts/test-ios.sh --tab-regression` alterna Register/History dez vezes após Success; a suíte também cobre o inventário normal de History e o identifier de retry. Existe regressão em `UIContentSizeCategoryAccessibilityExtraExtraExtraLarge`; ela complementa, mas não substitui, VoiceOver e validação manual.

O gate cross-platform `make verify` permanece separado e não exige macOS/Xcode.

## Segurança, privacidade e limitações

- somente fixtures sintéticas são autorizadas;
- nenhum dado financeiro é persistido localmente;
- não existem analytics, telemetria, crash SDK externo, cookies persistentes ou cache financeiro;
- não há autenticação, token, owner ID, Face ID, passkey, PIN ou Keychain;
- retry mantém a mesma chave somente para falhas transitórias ou de outcome incerto; `400` e `409` exigem edição/nova revisão;
- a chave idempotente sobrevive apenas enquanto a tentativa permanece em memória;
- recuperação após encerramento/restart depende de futura autenticação e armazenamento local seguro;
- Simulator loopback é o alvo seguro atual; acesso por dispositivo físico/LAN permanece planejado e não justifica enfraquecer o backend.

A UI aplica componentes e fontes do sistema, Dynamic Type, labels/hints VoiceOver, ordem semântica nativa, alvos mínimos de toque e Dark Mode. Isso é implementação de baseline, não alegação de conformidade WCAG 2.2 AA; validação manual com tecnologias assistivas e revisão independente ainda são obrigatórias.
