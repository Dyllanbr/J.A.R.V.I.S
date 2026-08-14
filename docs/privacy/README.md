# Privacidade

Estes documentos são baselines e templates de privacy by design. Eles não afirmam conformidade jurídica completa, não registram tratamentos inexistentes e não substituem revisão jurídica.

## Estado atual

É necessário distinguir três dimensões:

- **Campos modelados:** `Expense` já representa ID, UserID, descrição, valor, forma de pagamento, data/hora, timezone financeiro e origem. UserID é um identificador opaco do modelo e não é considerado intrinsecamente anônimo ou sintético.
- **Fixtures de teste:** os valores usados atualmente pelos testes automatizados são exclusivamente sintéticos.
- **Operação local/teste:** PostgreSQL e a API financeira opt-in persistem dados financeiros exclusivamente sintéticos em desenvolvimento e em bancos descartáveis de integração. A metadata idempotente é técnica e mínima. Isso valida comportamento, não representa tratamento de usuário real.
- **Cliente iOS:** a Etapa 3 mantém rascunho, preview e chave idempotente somente em memória, sem UserDefaults, banco local, analytics, telemetria ou owner. XCTest/XCUITest e a integração usam apenas fixtures sintéticas.
- **Tratamento de produto:** ainda não existem conta ou usuário real, armazenamento aprovado de dados financeiros reais, autenticação, beta externo ou integração financeira externa. A presença dos endpoints não autoriza uso com dados reais antes do LGPD Readiness Gate.

Enquanto o projeto for usado exclusivamente por pessoa natural para fins particulares e não econômicos, aplica-se a exceção do art. 4º, I, da LGPD. Essa exceção deve ser reavaliada se o contexto mudar e nunca autoriza ignorar segurança ou privacidade.

Qualquer beta com terceiros exige a conclusão e aprovação formal do [LGPD Readiness Gate](privacy-by-design-checklist.md) antes de usuários externos.

## Documentos

- [Baseline LGPD](lgpd-baseline.md)
- [Inventário de dados](data-inventory.md)
- [Atividades de tratamento](processing-activities.md)
- [Bases legais](legal-bases.md)
- [Retenção e eliminação](retention-and-deletion.md)
- [Direitos dos titulares](data-subject-rights.md)
- [Operadores e transferências](processors-and-transfers.md)
- [Checklist privacy by design e readiness gate](privacy-by-design-checklist.md)
- [Template de RIPD](ripd-template.md)

Referências oficiais: [Lei nº 13.709/2018](https://www.planalto.gov.br/ccivil_03/_ato2015-2018/2018/lei/l13709.htm) e [orientações da ANPD](https://www.gov.br/anpd/pt-br).
