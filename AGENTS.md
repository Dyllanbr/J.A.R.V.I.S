# Regras permanentes do repositório

Estas regras valem para pessoas e agentes que alterem o J.A.R.V.I.S.

## Engenharia e arquitetura

- Produzir código limpo, idiomático, legível, manutenível e proporcional ao incremento.
- Preservar o backend como monólito modular, com dependências explícitas.
- Manter domínio e casos de uso independentes de HTTP, banco de dados, IA, cloud e integrações externas.
- Não colocar regra de negócio em handlers ou controllers, nem SQL em handlers.
- Não permitir que IA ou MCP acessem banco de dados diretamente.
- Não criar pacote genérico `utils`; compartilhamento exige responsabilidade estável e nome claro.
- Não adicionar framework, dependência ou abstração sem necessidade concreta.
- Não implementar capacidade fora do incremento solicitado.

## Qualidade e proteção

- Segurança, privacidade, acessibilidade, performance, operabilidade e testabilidade são requisitos de primeira classe.
- Nunca versionar credencial, secret, chave privada, dado financeiro real ou dado pessoal real.
- Testes e fixtures usam somente valores sintéticos.
- Documentação, contrato e testes evoluem junto do comportamento observável.
- Mudanças que tratem dados pessoais devem passar pelos controles de privacy by design e, antes de beta externo, pelo LGPD Readiness Gate.
- WCAG 2.2 nível AA é a baseline mínima planejada para interfaces; testes automatizados não substituem validação manual com tecnologias assistivas.

## Estados e revisão independente

Toda tecnologia ou capacidade deve ser classificada como:

- **PLANEJADO:** decisão ou item documentado, sem implementação;
- **IMPLEMENTADO:** código ou documentação existe;
- **VERIFICADO:** passou pelos critérios aplicáveis e referencia evidência correspondente.

Agentes implementadores não aprovam autonomamente a própria entrega. Revisões de arquitetura, backend, QA, segurança, supply chain, acessibilidade, documentação, performance e operabilidade devem ser independentes quando aplicáveis ao risco.

## Comandos oficiais

Executar sempre a partir da raiz:

```bash
make bootstrap
make check
make verify
make smoke
make test-integration
make verify-ios
make test-ios-integration
```

`make verify` é o quality gate cross-platform/backend e equivalente ao job Linux do CI. `make verify-ios` é o gate macOS/Simulator com stub explícito; `make test-ios-integration` deve falhar fechado e comprovar app/URLSession reais contra API/PostgreSQL locais, incluindo pós-condição no banco. A entrega segue a [Definition of Done](docs/quality/definition-of-done.md); nada é chamado de verificado sem evidência, e findings bloqueadores impedem aprovação.

Comandos de desenvolvimento PostgreSQL (`make db-up`, `make migrate-up`, `make migrate-down` e `make db-down`) exigem configuração local explícita. `make verify` também exige Docker a partir da Etapa 2A.

## Limites por incremento

A fundação contém estrutura do monorepo, processo Go, configuração, lifecycle HTTP, `GET /healthz`, contrato e automação de qualidade. A Etapa 1 acrescentou somente `Money`, `Expense`, `CreateExpense` e suas portas. A Etapa 2A acrescentou PostgreSQL local/CI, migrations e o adapter de persistência atômica Expense + audit event. A Etapa 2B acrescentou somente idempotência transacional, preview, registro HTTP confirmado pelo canal e consulta mensal owner-scoped. A Etapa 3 acrescenta somente o primeiro app SwiftUI: entrada de Expense, preview/revisão/confirmação, sucesso e histórico mensal.

Permanecem fora da Etapa 3: autenticação, autorização multiusuário, armazenamento financeiro local, dispositivo físico/LAN, receitas, parcelamentos, orçamento, metas, categorias funcionais, IA, WhatsApp funcional, MCP, agentes de produto e infraestrutura cloud/Terraform funcional. O sistema registra despesas já ocorridas e nunca executa Pix, pagamentos ou transferências. App/API/banco local de teste não autorizam dados pessoais ou financeiros reais.
