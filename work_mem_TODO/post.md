# Briefing

Anos atrás, recebi a missão de investigar e **resolver** a lentidão em um sistema crítico da firma. Foram noites mal dormidas e alguns fios a menos de cabelo. O backend utilizava PostgreSQL e, depois de muito suor e investigação, a solução veio em literalmente uma linha:

```sql
ALTER USER foo SET work_mem='32MB';
```

Sendo sincero, este conteúdo pode ou não resolver seu problema de maneira imediata; vai depender muito do padrão das queries no seu sistema. Mas, se você trabalha com backend, espero que este post traga mais uma opção no seu arsenal para resolver problemas de performance, especialmente no PostgreSQL :smile:.

Vamos reproduzir um cenário favorável para a degradação de performance e explorar algumas ferramentas para investigar o problema em detalhe, como EXPLAIN, k6 para testes de carga e o método científico para examinar recursos computacionais — throughput de disco, CPU e memória. (TODO rever)


# Cenário

Vamos criar um sistema simples para analisar o desempenho de jogadores de futebol, por enquanto, a única necessidade de neǵocio é responder a seguinte pergunta:

- Qual o top N dos jogadores que mais tiveram participações em gols?

Para responder a essa pergunta, vamos modelar nosso banco de dados com três tabelas. O SQL a seguir cria e popula essas tabelas:

```sql
CREATE TABLE players (
    player_id SERIAL PRIMARY KEY,
    nationality TEXT,
    age INT,
    position TEXT
);

CREATE TABLE matches (
    match_id SERIAL PRIMARY KEY,
    match_date DATE,
    home_team TEXT,
    away_team TEXT
);

CREATE TABLE player_stats (
    player_stat_id SERIAL PRIMARY KEY,
    player_id INT REFERENCES players(player_id),
    match_id INT REFERENCES matches(match_id),
    goals INT,
    assists INT,
    minutes_played INT
);

-- Populate players with a range of nationalities, ages, and positions
INSERT INTO players (nationality, age, position)
SELECT
    ('Country' || (1 + random()*100)::int),  -- 100 different nationalities
    (18 + random()*20)::int,                 -- Ages between 18 and 38
    (ARRAY['Forward', 'Midfielder', 'Defender', 'Goalkeeper'])[ceil(random()*4)::int]
FROM generate_series(1, 10000);

-- Populate matches with random dates and team names
INSERT INTO matches (match_date, home_team, away_team)
SELECT
    CURRENT_DATE - (random()*365 * 2)::int,      -- Random date within the last two years
    ('Team' || (1 + random()*25)::int),      -- 50 different teams
    ('Team' || (25 + random()*25)::int)
FROM generate_series(1, 1000);

-- Populate player_stats with random stats for players in matches
-- Ensuring player_id is between 1 and 10,000, and match_id between 1 and 1,000
INSERT INTO player_stats (player_id, match_id, goals, assists, minutes_played)
SELECT
    (1 + trunc(random()*9999))::int,         -- Random player_id between 1 and 10,000
    (1 + trunc(random()*999))::int,          -- Random match_id between 1 and 1,000
    (random()*3)::int,                       -- Goals between 0 and 2
    (random()*2)::int,                       -- Assists between 0 and 1
    (45 + random()*45)::int                  -- Minutes played between 45 and 90
FROM generate_series(1, 100000);
```

O script para inicializar e popular o banco de dados está no [repositório do GitHub](link do projeto).

> E sim, poderíamos modelar os dados de forma que as queries fossem mais performáticas, mas o objetivo aqui é justamente explorar cenários não otimizados. Acredite, você provavelmente vai encontrar sistemas onde é preciso "tirar leite de pedra" para conseguir performance, seja por erros de modelagem ou crescimento inesperado.

# Debugando o Problema

Para simular o problema com work_mem, vamos formular a query para responder: Qual o top 2000 dos jogadores que mais contribuíram com gols? Ou seja, o somatório de assistências e gols por player_id, ordenado de forma decrescente:

```sql
SELECT p.player_id, SUM(ps.goals + ps.assists) AS total_score
FROM player_stats ps
JOIN players p ON ps.player_id = p.player_id
GROUP BY p.player_id
ORDER BY total_score DESC
LIMIT 2000;
```

Tá mas como identifico possíveis gargalos nessa query? Não só o PostgreSQL mas como outros DBMS do mercado suportam o comando ***[EXPLAIN](https://www.postgresql.org/docs/current/sql-explain.html)*** que detalha a sequência de passos, ou o plano de execução otimizado, que precisará ser percorrido e executado dado a query em questão. Podemos ver informações como:

- Qual o tipo da busca? Index scan, Index Only scan, Seq Scan, etc.
- Qual índice foi utilizado? E por qual condição?
- Se houver _Sort_, qual foi o algoritmo utilizado? Utilizou memória ou disco? 
- Uso de _[shared buffers](https://postgresqlco.nf/doc/en/param/shared_buffers/)_.
- Estimativas de tempo de execução.

Você encontra mais sobre o planner/optimizer na documentação oficial: https://www.postgresql.org/docs/current/planner-optimizer.html.

## Talk is cheap

Falar é fácil então vamos análisar um exemplo prático. Primeiro vamos diminuir o work_mem para o menor valor possível, que é 64kB como podemos ver no [código fonte](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L695) e em seguida analisar o output do `EXPLAIN`:

```sql
BEGIN; -- 1. Iniciando uma transação.

SET LOCAL work_mem = '64kB'; -- 2. Alterando o valor de work_mem a nível de transação, outras transações na mesma sessão utilizará o valor default ou pré configurado.

EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS) -- 3. Explain incluindo opções que ajudam a identificar gargalos, para mais informações olhe a seção de referências. 
SELECT 
    p.player_id, 
    SUM(ps.goals + ps.assists) AS total_score 
FROM 
    player_stats ps
INNER JOIN 
    players p ON p.player_id = ps.player_id
GROUP BY 
    p.player_id
ORDER BY 
    total_score DESC
LIMIT 2000;
--

QUERY PLAN                                                                                                                                                          |
--------------------------------------------------------------------------------------------------------------------------------------------------------------------+
Limit  (cost=18978.96..18983.96 rows=2000 width=12) (actual time=81.589..81.840 rows=2000 loops=1)                                                                  |
  Output: p.player_id, (sum((ps.goals + ps.assists)))                                                                                                               |
  Buffers: shared hit=667, temp read=860 written=977                                                                                                                |
  ->  Sort  (cost=18978.96..19003.96 rows=10000 width=12) (actual time=81.587..81.724 rows=2000 loops=1)                                                            |
        Output: p.player_id, (sum((ps.goals + ps.assists)))                                                                                                         |
        Sort Key: (sum((ps.goals + ps.assists))) DESC                                                                                                               |
        Sort Method: external merge  Disk: 280kB                                                                                                                    |
        Buffers: shared hit=667, temp read=860 written=977                                                                                                          |
        ->  GroupAggregate  (cost=15076.66..17971.58 rows=10000 width=12) (actual time=40.293..79.264 rows=9998 loops=1)                                            |
              Output: p.player_id, sum((ps.goals + ps.assists))                                                                                                     |
              Group Key: p.player_id                                                                                                                                |
              Buffers: shared hit=667, temp read=816 written=900                                                                                                    |
              ->  Merge Join  (cost=15076.66..17121.58 rows=100000 width=12) (actual time=40.281..71.313 rows=100000 loops=1)                                       |
                    Output: p.player_id, ps.goals, ps.assists                                                                                                       |
                    Merge Cond: (p.player_id = ps.player_id)                                                                                                        |
                    Buffers: shared hit=667, temp read=816 written=900                                                                                              |
                    ->  Index Only Scan using players_pkey on public.players p  (cost=0.29..270.29 rows=10000 width=4) (actual time=0.025..1.014 rows=10000 loops=1)|
                          Output: p.player_id                                                                                                                       |
                          Heap Fetches: 0                                                                                                                           |
                          Buffers: shared hit=30                                                                                                                    |
                    ->  Materialize  (cost=15076.32..15576.32 rows=100000 width=12) (actual time=40.250..57.942 rows=100000 loops=1)                                |
                          Output: ps.goals, ps.assists, ps.player_id                                                                                                |
                          Buffers: shared hit=637, temp read=816 written=900                                                                                        |
                          ->  Sort  (cost=15076.32..15326.32 rows=100000 width=12) (actual time=40.247..49.339 rows=100000 loops=1)                                 |
                                Output: ps.goals, ps.assists, ps.player_id                                                                                          |
                                Sort Key: ps.player_id                                                                                                              |
                                Sort Method: external merge  Disk: 2208kB                                                                                           |
                                Buffers: shared hit=637, temp read=816 written=900                                                                                  |
                                ->  Seq Scan on public.player_stats ps  (cost=0.00..1637.00 rows=100000 width=12) (actual time=0.011..8.378 rows=100000 loops=1)    |
                                      Output: ps.goals, ps.assists, ps.player_id                                                                                    |
                                      Buffers: shared hit=637                                                                                                       |
Planning:                                                                                                                                                           |
  Buffers: shared hit=6                                                                                                                                             |
Planning Time: 0.309 ms                                                                                                                                             |
Execution Time: 82.718 ms                                                                                                                                    |

COMMIT; -- 4. Aqui poderia ser um ROLLBACK, possibilitando analisar queries de INSERT, UPDATE e DELETE.
```

Podemos ver que o tempo de execução da query foi de **82.718ms** e que o algoritmo de sort utilizado foi o external merge, que usa disco em vez de memória dado que o conjunto de dados ultrapassou o 64kB de _work_mem_ configurado.

Por curiosidade, o módulo tuplesort.c marca que o algoritmo de sort irá utilizar disco setando o estado para _SORTEDONTAPE_ [nessa linha](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L1394) e as iterações com o disco é exposto pelo módulo [logtape.c](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/logtape.c).

Se você é uma pessoa mais visual (como eu) existem ferramentas que facilitam o entendimento do output do EXPLAIN como https://explain.dalibo.com/, a imagem a seguir mostra um dos nós com a etapa de Sort incluindo detalhes como `Sort Method: external merge` e `Sort Space Used: 2.2MB`:

![alt text](explain-work-mem-64kb.png)

A parte de "Stats" é super útil para queries mais complexas, mostrando o quanto cada nó contribuiu em tempo de execução da query, no nosso exemplo, ele já indicaria um potencial suspeito que são nós do tipo Sort que juntos levam ~42ms:

![alt text](explain-stats-64kb.png)

- Você pode analisar e visualizar o explain dessa query no link: https://explain.dalibo.com/plan/2gd0a8c8fab6a532#stats

Como indicado pelo EXPLAIN um dos principais problemas de performance nessa query é o nó de Sort que está utilizando disco, inclusive, um efeito colateral que pode ser observado, principalmente se vc trabalhar com sistemas que possui uma quantidade considerável de usuários, é picos na métrica de Write I/O (espero que vc tenha métricas, caso contrário sinta-se abraçado), sim, a query de leitura pode causar spikes de escrita pq o algoritmo de Sort que utiliza disco escreve em arquivos temporários.

## Solução

Se rodarmos a mesma query setando work_mem=4MB, o tempo de execução cai em aproximadamente 50%:

```sql
EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS) 
SELECT 
    p.player_id, 
    SUM(ps.goals + ps.assists) AS total_score 
FROM 
    player_stats ps
INNER JOIN 
    players p ON p.player_id = ps.player_id
GROUP BY 
    p.player_id
ORDER BY 
    total_score DESC
LIMIT 2000;
--
QUERY PLAN                                                                                                                                          |
----------------------------------------------------------------------------------------------------------------------------------------------------+
Limit  (cost=3646.90..3651.90 rows=2000 width=12) (actual time=41.672..41.871 rows=2000 loops=1)                                                    |
  Output: p.player_id, (sum((ps.goals + ps.assists)))                                                                                               |
  Buffers: shared hit=711                                                                                                                           |
  ->  Sort  (cost=3646.90..3671.90 rows=10000 width=12) (actual time=41.670..41.758 rows=2000 loops=1)                                              |
        Output: p.player_id, (sum((ps.goals + ps.assists)))                                                                                         |
        Sort Key: (sum((ps.goals + ps.assists))) DESC                                                                                               |
        Sort Method: top-N heapsort  Memory: 227kB                                                                                                  |
        Buffers: shared hit=711                                                                                                                     |
        ->  HashAggregate  (cost=2948.61..3048.61 rows=10000 width=12) (actual time=38.760..40.073 rows=9998 loops=1)                               |
              Output: p.player_id, sum((ps.goals + ps.assists))                                                                                     |
              Group Key: p.player_id                                                                                                                |
              Batches: 1  Memory Usage: 1169kB                                                                                                      |
              Buffers: shared hit=711                                                                                                               |
              ->  Hash Join  (cost=299.00..2198.61 rows=100000 width=12) (actual time=2.322..24.273 rows=100000 loops=1)                            |
                    Output: p.player_id, ps.goals, ps.assists                                                                                       |
                    Inner Unique: true                                                                                                              |
                    Hash Cond: (ps.player_id = p.player_id)                                                                                         |
                    Buffers: shared hit=711                                                                                                         |
                    ->  Seq Scan on public.player_stats ps  (cost=0.00..1637.00 rows=100000 width=12) (actual time=0.008..4.831 rows=100000 loops=1)|
                          Output: ps.player_stat_id, ps.player_id, ps.match_id, ps.goals, ps.assists, ps.minutes_played                             |
                          Buffers: shared hit=637                                                                                                   |
                    ->  Hash  (cost=174.00..174.00 rows=10000 width=4) (actual time=2.298..2.299 rows=10000 loops=1)                                |
                          Output: p.player_id                                                                                                       |
                          Buckets: 16384  Batches: 1  Memory Usage: 480kB                                                                           |
                          Buffers: shared hit=74                                                                                                    |
                          ->  Seq Scan on public.players p  (cost=0.00..174.00 rows=10000 width=4) (actual time=0.004..0.944 rows=10000 loops=1)    |
                                Output: p.player_id                                                                                                 |
                                Buffers: shared hit=74                                                                                              |
Planning:                                                                                                                                           |
  Buffers: shared hit=6                                                                                                                             |
Planning Time: 0.236 ms                                                                                                                             |
Execution Time: 41.998 ms                                                                                                                           |
```

- Se preferir visualmente: https://explain.dalibo.com/plan/b094ec2f1cfg44f6#


Perceba que nesse explain, um dos nós de Sort passou a utilizar um algoritmo que faz sort em memória _heapsort_, por curiosidade, o planner decide por um heapsort somente se achar barato o suficiente em vez de mandar um quicksort, mais detalhes no [código fonte](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L1229-L1252).


Além disso, o segundo nó de Sort que tomava mais tempo, aprox. 40ms simplesmente desapareceu do plano de execução da query, isso aconteceu pq o planner elencou um nó de `HashJoin` em vez de `MergeJoin` dado que agora a operação de hash cabe tranquilamente em memória para essa query que utilizou 480kB. Mais detalhes sobre nesses artigos:

- [HashJoin Algorithm](https://postgrespro.com/blog/pgsql/5969673)
- [MergeJoin Algorithm](https://postgrespro.com/blog/pgsql/5969770)

# Double check na solução (mudar titulo?)

# Conclusão

- cautela ao subir o valor de work_mem, dado que pode ser multiplicado pelo número de nós utilizado pela query
- comentar sobre pooler, talvez tenha um controle mais previsivel sobre conexões e seja mais easy de calcular um work_mem seguro
- via de regra o valor default é mt baixo
- é possível alterar a nível de transação de banco para queries especificas


TODO

[x] script to populate the database
[x] queries to simulate the problem -- cant do it yet...
[x] deep dive in sorting methods of PostgreSQL, algorithms that used memory vs used disk
[x] also mentioning postgresql codebase
[] practical example with a simple API and k6
[] revisar links

