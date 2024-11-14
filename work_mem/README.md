# Desbravando work_mem: Otimização de queries no PostgreSQL

Um exemplo de caso que explora problemas de performance causados pelo valor configurado do `work_mem` no PostgreSQL. Esse parâmetro pode impactar operações de `Sort`, `Joins`, e até influenciar o planner a escolher nós mais eficientes na execução das queries. Mais detalhes na [documentação oficial](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-WORK-MEM).

## Executando os testes :scroll:

- `docker compose up --build --abort-on-container-exit`

**Nota:** altere a variável de ambiente `ENDPOINT` no docker-compose.yaml entre `/low-work-mem` e `/optimized-work-mem` para estressar os dois cenários.

### Output Esperado

```sh

|      ✓ status is 200
| 
|      checks.........................: 100.00% 4275 out of 4275
|      data_received..................: 321 kB  7.1 kB/s
|      data_sent......................: 393 kB  8.7 kB/s
|      http_req_blocked...............: avg=7.18µs  min=1.71µs  med=5.35µs  max=551.7µs  p(90)=7.45µs   p(95)=8.68µs  
|      http_req_connecting............: avg=630ns   min=0s      med=0s      max=448.7µs  p(90)=0s       p(95)=0s      
|      http_req_duration..............: avg=71.77ms min=29.99ms med=76.67ms max=168.83ms p(90)=95.3ms   p(95)=100.53ms
|      { expected_response:true }...: avg=71.77ms min=29.99ms med=76.67ms max=168.83ms p(90)=95.3ms   p(95)=100.53ms
|      http_req_failed................: 0.00%   0 out of 4275
|      http_req_receiving.............: avg=90.41µs min=13.88µs med=77.02µs max=3.68ms   p(90)=115.28µs p(95)=159.52µs
|      http_req_sending...............: avg=21.4µs  min=6.39µs  med=18.21µs max=612.19µs p(90)=27.02µs  p(95)=32.85µs 
|      http_req_tls_handshaking.......: avg=0s      min=0s      med=0s      max=0s       p(90)=0s       p(95)=0s      
|      http_req_waiting...............: avg=71.66ms min=29.9ms  med=76.55ms max=168.71ms p(90)=95.18ms  p(95)=100.4ms 
|      http_reqs......................: 4275    94.931194/s
|      iteration_duration.............: avg=71.99ms min=30.17ms med=76.9ms  max=169.05ms p(90)=95.5ms   p(95)=100.74ms
|      iterations.....................: 4275    94.931194/s
|      vus............................: 1       min=1    max=10
|      vus_max........................: 10      min=10   max=10
| 
| 
| running (0m45.0s), 00/10 VUs, 4275 complete and 0 interrupted iterations
| default ✓ [ 100% ] 00/10 VUs  45s
```

### Versão do Docker

```text
Client: Docker Engine - Community
 Version:           20.10.22
 API version:       1.41
 Go version:        go1.18.9
 Git commit:        3a2c30b
 Built:             Thu Dec 15 22:28:04 2022
 OS/Arch:           linux/amd64
 Context:           default
 Experimental:      true
 ```

