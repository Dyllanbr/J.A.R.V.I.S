# Baseline de performance e operabilidade

## Estado

Ainda não há SLOs de produto, carga representativa ou resultado comprovado de performance. Latência, throughput, disponibilidade e consumo de recursos são metas **PLANEJADAS** a definir quando existirem jornadas e ambientes reais; não são alegações atuais.

## Operação implementada

- startup valida configuração e registra o endereço ligado sem secrets;
- `GET /healthz` confirma somente que o processo HTTP responde e não consulta dependências inexistentes;
- limites de cabeçalho e timeouts de leitura, escrita, cabeçalhos e idle são explícitos;
- o primeiro `SIGINT` ou `SIGTERM` inicia shutdown gracioso com limite configurável de até 30 segundos;
- um segundo sinal pode forçar o comportamento padrão do sistema;
- o smoke aguarda o PID recém-iniciado, envia `SIGTERM`, executa `wait` e usa `SIGKILL` apenas após timeout controlado;
- logs estruturados são úteis para lifecycle e não incluem corpo, dado pessoal, dado financeiro ou configuração sensível.
- o modo financeiro usa pool bounded, timeout por operação e query mensal indexada/ordenada; isso é baseline de operabilidade, não resultado de performance;
- o modo health-only não abre pool, e o lifecycle E2E encerra API, container e volume em sucesso ou falha.
- o cliente iOS usa async/await e URLSession sem bloquear a thread principal; nenhum resultado de latência, memória ou energia foi alegado. O gate do Simulator é funcional, não benchmark.

O binário usa configuração por ambiente e não depende de paths absolutos ou recursos temporários para funcionar. Isso é compatível com futura containerização, mas nenhum container ou infraestrutura foi implementado.

Medições futuras devem especificar workload, dataset sintético, hardware, ambiente e evidência. Perfis e medições precedem otimizações.
