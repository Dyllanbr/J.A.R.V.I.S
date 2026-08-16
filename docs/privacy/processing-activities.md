# Registro de atividades de tratamento

Estado: tratamento de produto com dados reais **PLANEJADO** e inexistente. Existe somente validação técnica local/CI com fixtures sintéticas em PostgreSQL; ela não cria conta, usuário externo, coleta ou compartilhamento.

| Atividade | Dados | Ambiente | Estado | Observação |
| --- | --- | --- | --- | --- |
| Teste da persistência de Expense, Income e Category | Fixtures sintéticas do inventário | Máquina do desenvolvedor e runner CI descartável | IMPLEMENTADO | Banco por teste; migrations 001–004; cleanup automático de integração; sem dado pessoal real |
| Teste E2E da API financeira, catálogo e idempotência | Fixtures e chaves técnicas sintéticas do inventário | Máquina do desenvolvedor e runner CI descartável | IMPLEMENTADO | Owner single-user sintético; Category de sistema; API/PostgreSQL encerrados e volume removido; sem dado pessoal real |
| Teste do cliente iOS | Rascunho, Category, preview, chave técnica e resposta exclusivamente sintéticos | iPhone Simulator e memória volátil; integração local com API/PostgreSQL descartáveis | IMPLEMENTADO | Sem armazenamento local, owner, telemetria ou dados reais; cleanup automatizado |
| Registro de Expense ou Income de usuário real | A definir | A definir | PLANEJADO | Bloqueado pelo LGPD Readiness Gate e por decisões ainda ausentes; Income registra uma entrada já ocorrida, não recebe dinheiro |

Cada atividade futura deve registrar:

| Campo | Conteúdo esperado |
| --- | --- |
| Identificador e estado | Nome estável; PLANEJADO, IMPLEMENTADO ou VERIFICADO |
| Papéis | Controlador, operadores e responsabilidades confirmadas |
| Titulares e dados | Referências ao inventário, sem copiar dados reais |
| Finalidade | Objetiva, específica e aprovada |
| Operações e fluxo | Coleta, uso, armazenamento, compartilhamento e eliminação |
| Base legal | Referência à análise documentada, nunca presumida |
| Retenção | Critério e justificativa aprovados |
| Controles e riscos | Salvaguardas técnicas e organizacionais |
| Evidência | Contrato, teste, revisão e aprovação aplicáveis |

Não extrapolar o teste sintético para afirmar tratamento real, base legal, fornecedor, transferência, retenção ou conformidade.
