# Briefing

Years ago, I was tasked with solving a performance issue in a critical system for the company I worked at. It was a tough challenge, sleepless nights and even more hair loss, The backend used PostgreSQL, and after a lot of effort and digging, the solution turned out to be as simple as one line:

```sql
ALTER USER foo SET work_mem='32MB';
```

Now, to be honest, this might or might not solve your performance issue right away. It depends heavily on your query patterns and the workload of your system. However, if you're a backend developer, I hope this post adds another tool to your arsenal for tackling problems, especially with PostgreSQL :smile:


In this post, we’ll create a scenario to simulate performance degradation and explore some tools to investigate the problem, like EXPLAIN, k6 for load testing, and even a dive into PostgreSQL’s source code. I’ll also share some articles to help you solve related issues.

- :arrow_right: [github repository with the complete implementation](https://github.com/iamseki/postgresql/tree/main/work_mem)

# Case Study

Let’s create a simple system to analyze soccer player performance. For now, the only business rule is to answer this question:

- Who are the top N players involved in scoring the most?

The following SQL creates our data model and populates it:

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
```

The script to initialize and populate the database is available in the [github repository](https://github.com/iamseki/postgresql/blob/main/work_mem/init_data.sql).

> Yes, we could design out database to improve system performance, but the main goal here is to explore unoptimized scenarios. Trust me, you'll likely encounter systems like this, where either poor initial design choices or unexpected growth require significant effort to improve performance.

# Debugging the problem

To simulate the issue related to the work_mem configuration, let’s create a query to answer this question: Who are the top 2000 players contributing the most to goals?

```sql
SELECT p.player_id, SUM(ps.goals + ps.assists) AS total_score
FROM player_stats ps
JOIN players p ON ps.player_id = p.player_id
GROUP BY p.player_id
ORDER BY total_score DESC
LIMIT 2000;
```

Alright, but how can we identify bottlenecks in this query? Like other DBMSs, PostgreSQL supports the ***[EXPLAIN](https://www.postgresql.org/docs/current/sql-explain.html)*** command, which helps us understand each step executed by the query planner (optimized or not).

We can analyze details such as:

- What kind of scan was used? Index scan, Index Only scan, Seq Scan, etc.
- Which index was used, and under what conditions?
- If sorting is involved, what type of algorithm was used? Does it rely entirely on memory, or does it require disk usage?
- The usage of _[shared buffers](https://postgresqlco.nf/doc/en/param/shared_buffers/)_.
- Execution time estimation.

You can learn more about the PostgreSQL planner/optimizer here:

- [official documentation](https://www.postgresql.org/docs/current/planner-optimizer.html)
- [pganalyze - basics of postgres query planning](https://pganalyze.com/docs/explain/basics-of-postgres-query-planning)
- [cybertec - how to interpret postgresql explain](https://www.cybertec-postgresql.com/en/how-to-interpret-postgresql-explain-analyze-output/?gad_source=1&gclid=CjwKCAiAudG5BhAREiwAWMlSjISvgthrORt-LxBH8K9hUhqvJ8B228ZBvHX9dM4MYD1xJ4iT6Z7P2BoCgTQQAvD_BwE)

## Talk is cheap

Talk is cheap, so let’s dive into a practical example. First, we’ll reduce the work_mem to its smallest possible value, which is 64kB, as defined in the [source code](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L695):


```C
	/*
	 * workMem is forced to be at least 64KB, the current minimum valid value
	 * for the work_mem GUC.  This is a defense against parallel sort callers
	 * that divide out memory among many workers in a way that leaves each
	 * with very little memory.
	 */
	state->allowedMem = Max(workMem, 64) * (int64) 1024;
```

Next, let’s analyze the output of the `EXPLAIN` command:


```sql
BEGIN; -- 1. Initialize a transaction.

SET LOCAL work_mem = '64kB'; -- 2. Change work_mem at transaction level, another running transactions at the same session will have the default value(4MB).

SHOW work_mem; -- 3. Check the modified work_mem value.

EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS) -- 4. Run explain with options that help us to analyses and indetifies bottlenecks. 
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

COMMIT; -- 5. You can also execute a ROLLBACK, in case you want to analyze queries like INSERT, UPDATE and DELETE.
```

We can see that the execution time was **82.718ms**, and the _Sort Algorithm_ used was `external merge`. This algorithm relies on disk instead of memory because the data exceeded the 64kB `work_mem` limit.

For your information, the tuplesort.c module flags when the Sort algorithm will use disk by setting the state to SORTEDONTAPE [at this line](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L1394). Disk interactions is handled by the [logtape.c](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/logtape.c) module.


If you're a visual person (like me), there are tools that can help you understand the EXPLAIN output, such as https://explain.dalibo.com/. Below is an example showing a node with the Sort step, including details like `Sort Method: external merge` and `Sort Space Used: 2.2MB`:

![explain dalibo work-mem 64kb](https://raw.githubusercontent.com/iamseki/postgresql/refs/heads/main/work_mem/explain-work-mem-64kb.png)

The "Stats" section is especially useful for analyzing more complex queries, as it provides execution time details for each query node. In our example, it highlights a suspiciously high execution time—nearly 42ms—in one of the Sort nodes:


![explain dalibo work-mem 64kb stats](https://raw.githubusercontent.com/iamseki/postgresql/refs/heads/main/work_mem/explain-stats-64kb.png)

- You can visualize and analyze this query plan here: https://explain.dalibo.com/plan/2gd0a8c8fab6a532#stats

As the `EXPLAIN` output shows, one of the main reasons for the performance problem is the Sort node using disk. A side effect of this issue, especially in systems with high workloads, is spikes in Write I/O metrics (I hope you’re monitoring these; if not, good luck when you need them!). And yes, even read-only queries can cause write spikes, as the Sort algorithm writes data to temporary files.


## Solution

When we execute the same query with work_mem=4MB (the default in PostgreSQL), the execution time decreases by over 50%.

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

- For a visual analysis, check this link: https://explain.dalibo.com/plan/b094ec2f1cfg44f6#

In this EXPLAIN output, one of the Sort nodes now uses an in-memory algorithm, heapsort. For context, the planner opts for heapsort only when it’s cheaper to execute than quicksort. You can dive deeper into the decision-making process in the [PostgreSQL source code](https://github.com/postgres/postgres/blob/master/src/backend/utils/sort/tuplesort.c#L1229-L1252).


Additionally, the second Sort node, which previously accounted for almost 40ms of execution time, disappears entirely from the execution plan. This change occurs because the planner now selects a HashJoin instead of a MergeJoin, as the hash operation fits in memory, consuming approximately 480kB.

For more details about join algorithms, check out these articles:

- [HashJoin Algorithm](https://postgrespro.com/blog/pgsql/5969673)
- [MergeJoin Algorithm](https://postgrespro.com/blog/pgsql/5969770)

### Impact on the API

The default work_mem isn’t always sufficient to handle your system’s workload. You can adjust this value at the user level using:

```sql
ALTER USER foo SET work_mem='32MB';
```

**Note:** If you’re using a connection pool or a connection pooler, it’s important to recycle old sessions for the new configuration to take effect.

You can also control this configuration at the database transaction level. Let’s run a simple API to understand and measure the impact of work_mem changes using load testing with [k6](https://k6.io/):


- `k6-test.js`

    ```js
        import http from 'k6/http';
        import { check } from 'k6';

        const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
        const ENDPOINT = __ENV.ENDPOINT || '/low-work-mem';

        export const options = {
        stages: [
            { duration: '15s', target: 10 }, // ramp up to 10 users
            { duration: '15s', target: 10 },
            { duration: '15s', target: 0 },  // ramp down to 0 users
        ],
        };

        export default function () {
        const res = http.get(`${BASE_URL}${ENDPOINT}`);
        check(res, { 'status is 200': (r) => r.status === 200 });
        }
    ```

The API was implemented in Go and exposes two endpoints that execute the query with different work_mem configurations:

- `main.go`

    ```go
        package main

        import (
            "context"
            "fmt"
            "log"
            "net/http"
            "os"

            "github.com/jackc/pgx/v5/pgxpool"
        )

        func main() {
            ctx := context.Background()

            dbURL := os.Getenv("POSTGRES_URL")
            if dbURL == "" {
                dbURL = "postgres://local:local@localhost:5432/local?pool_max_conns=100&pool_min_conns=10"
            }

            pgx, err := pgxpool.New(ctx, dbURL)
            if err != nil {
                log.Fatalf("Unable to connect to database: %s", err)
            }
            defer pgx.Close()

            top_players_query := `
                SELECT p.player_id, SUM(ps.goals + ps.assists) AS total_score
                FROM player_stats ps
                JOIN players p ON ps.player_id = p.player_id
                GROUP BY p.player_id
                ORDER BY total_score DESC
                LIMIT 2000;
            `

            http.HandleFunc("/optimized-work-mem", func(w http.ResponseWriter, r *http.Request) {
                _, err := pgx.Exec(ctx, top_players_query)
                if err != nil {
                    http.Error(w, fmt.Sprintf("Query error: %v", err), http.StatusInternalServerError)
                    return
                }

                log.Println("Successfully executed query with work_mem=4MB")

                w.WriteHeader(http.StatusOK)
            })

            http.HandleFunc("/low-work-mem", func(w http.ResponseWriter, r *http.Request) {
                query := fmt.Sprintf(`
                BEGIN;		
                SET LOCAL work_mem = '64kB';
                %s
                COMMIT;
                `, top_players_query)

                _, err := pgx.Exec(ctx, query)
                if err != nil {
                    http.Error(w, fmt.Sprintf("Query error: %v", err), http.StatusInternalServerError)
                    return
                }

                log.Println("Successfully executed query with work_mem=64kB")

                w.WriteHeader(http.StatusOK)
            })

            log.Println("Starting server on port 8082")
            log.Fatal(http.ListenAndServe("0.0.0.0:8082", nil))
        }
    ```

Below is the docker-compose file containing all the dependencies needed to run the load test:

- `docker-compose.yaml`

    ```yaml
        version: "3.8"

        services:
        postgres:
            image: postgres:17
            environment:
            POSTGRES_PASSWORD: local
            POSTGRES_USER: local
            POSTGRES_DB: local
            healthcheck:
            test: ["CMD-SHELL", "pg_isready"]
            interval: 10s
            timeout: 5s
            retries: 5
            volumes:
            - ./init_data.sql:/docker-entrypoint-initdb.d/init_data.sql # Initialize the database
            - ./data:/var/lib/postgresql/data  # Mounts the local "data" directory to the container's data directory

            ports:
            - "5432:5432"

        api:
            build:
            context: .
            dockerfile: Dockerfile.api
            environment:
            POSTGRES_URL: postgres://local:local@postgres:5432/local?pool_max_conns=100&pool_min_conns=10
            ports:
            - "8082:8082"
            depends_on:
            - postgres

        k6:
            image: grafana/k6
            entrypoint: [ "k6", "run", "/scripts/k6-test.js" ]
            environment:
            BASE_URL: http://api:8082
            ENDPOINT: /low-work-mem
            volumes:
            - ./k6-test.js:/scripts/k6-test.js
            depends_on:
            - api

        volumes:
        postgres:
    ```

We can set the ENDPOINT environment variable to define the scenario to test: /low-work-mem or /optimized-work-mem. Run the test using: `docker compose up --abort-on-container-exit`. For this example, I used Docker version 20.10.22.

- Test `ENDPOINT: /low-work-mem` - `work_mem=64kB`

    ```sh
        ============ 64kB work_mem k6 output =============
        | 
        |      ✓ status is 200
        | 
        |      checks.........................: 100.00% 2846 out of 2846
        |      data_received..................: 214 kB  4.7 kB/s
        |      data_sent......................: 245 kB  5.4 kB/s
        |      http_req_blocked...............: avg=8.78µs   min=2.13µs  med=5.25µs   max=2.62ms   p(90)=7.19µs   p(95)=8.1µs   
        |      http_req_connecting............: avg=1.04µs   min=0s      med=0s       max=429.38µs p(90)=0s       p(95)=0s      
        |      http_req_duration..............: avg=108ms    min=61.55ms med=113.45ms max=198.55ms p(90)=138.63ms p(95)=143.01ms
        |        { expected_response:true }...: avg=108ms    min=61.55ms med=113.45ms max=198.55ms p(90)=138.63ms p(95)=143.01ms
        |      http_req_failed................: 0.00%   0 out of 2846
        |      http_req_receiving.............: avg=84.49µs  min=18.43µs med=73.4µs   max=1.08ms   p(90)=109.13µs p(95)=149.98µs
        |      http_req_sending...............: avg=22.68µs  min=6.49µs  med=17.67µs  max=2.37ms   p(90)=25.87µs  p(95)=30.11µs 
        |      http_req_tls_handshaking.......: avg=0s       min=0s      med=0s       max=0s       p(90)=0s       p(95)=0s      
        |      http_req_waiting...............: avg=107.89ms min=61.45ms med=113.35ms max=198.4ms  p(90)=138.52ms p(95)=142.91ms
        |      http_reqs......................: 2846    63.204112/s
        |      iteration_duration.............: avg=108.2ms  min=61.71ms med=113.83ms max=198.77ms p(90)=138.93ms p(95)=143.17ms
        |      iterations.....................: 2846    63.204112/s
        |      vus............................: 1       min=1            max=10
        |      vus_max........................: 10      min=10           max=10
        | 
        | 
        | running (0m45.0s), 00/10 VUs, 2846 complete and 0 interrupted iterations
        | default ✓ [ 100% ] 00/10 VUs  45s

    ```

- Test `ENDPOINT: /optimized-work-mem` - `work_mem=4MB`

    ```sh
        ============ 4MB work_mem k6 output =============
        |      ✓ status is 200
        | 
        |      checks.........................: 100.00% 4275 out of 4275
        |      data_received..................: 321 kB  7.1 kB/s
        |      data_sent......................: 393 kB  8.7 kB/s
        |      http_req_blocked...............: avg=7.18µs  min=1.71µs  med=5.35µs  max=551.7µs  p(90)=7.45µs   p(95)=8.68µs  
        |      http_req_connecting............: avg=630ns   min=0s      med=0s      max=448.7µs  p(90)=0s       p(95)=0s      
        |      http_req_duration..............: avg=71.77ms min=29.99ms med=76.67ms max=168.83ms p(90)=95.3ms   p(95)=100.53ms
        |        { expected_response:true }...: avg=71.77ms min=29.99ms med=76.67ms max=168.83ms p(90)=95.3ms   p(95)=100.53ms
        |      http_req_failed................: 0.00%   0 out of 4275
        |      http_req_receiving.............: avg=90.41µs min=13.88µs med=77.02µs max=3.68ms   p(90)=115.28µs p(95)=159.52µs
        |      http_req_sending...............: avg=21.4µs  min=6.39µs  med=18.21µs max=612.19µs p(90)=27.02µs  p(95)=32.85µs 
        |      http_req_tls_handshaking.......: avg=0s      min=0s      med=0s      max=0s       p(90)=0s       p(95)=0s      
        |      http_req_waiting...............: avg=71.66ms min=29.9ms  med=76.55ms max=168.71ms p(90)=95.18ms  p(95)=100.4ms 
        |      http_reqs......................: 4275    94.931194/s
        |      iteration_duration.............: avg=71.99ms min=30.17ms med=76.9ms  max=169.05ms p(90)=95.5ms   p(95)=100.74ms
        |      iterations.....................: 4275    94.931194/s
        |      vus............................: 1       min=1            max=10
        |      vus_max........................: 10      min=10           max=10
        | 
        | 
        | running (0m45.0s), 00/10 VUs, 4275 complete and 0 interrupted iterations
        | default ✓ [ 100% ] 00/10 VUs  45s
    ```

The results demonstrate that the endpoint with a higher work_mem outperformed the one with a lower configuration. The p90 latency dropped by over 43ms, and throughput improved significantly under the test workload.

If percentile metrics are new to you, I recommend studying and understanding them. These metrics are incredibly helpful for guiding performance analyses. Here are some resources to get you started:

- [k6 response time](https://github.com/grafana/k6-learn/blob/main/Modules/II-k6-Foundations/03-Understanding-k6-results.md#response-time)
- [p90 vs p99](https://www.akitasoftware.com/blog-posts/p90-vs-p99-why-not-both#:~:text=The%20p90%20latency%20of%20an,were%20faster%20than%20this%20number.)

# Conclusion

After dreaming about the problem, waking up multiple times to try new solutions, and finally discovering that `work_mem` could help, the next challenge is figuring out the right value for this configuration.  :grimacing:


The default value of 4MB for work_mem, like many other PostgreSQL settings, is conservative. This allows PostgreSQL to run on smaller machines with limited computational power. However, we must be cautious not to crash the PostgreSQL instance with out-of-memory errors. **A single query, if complex enough, can consume multiple times the memory specified by work_mem**, depending on the number of operations like Sorts, Merge Joins, Hash Joins (influenced by _hash_mem_multiplier_), and more. As noted in the [official documentation](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-WORK-MEM):

>it is necessary to keep this fact in mind when choosing the value. Sort operations are used for ORDER BY, DISTINCT, and merge joins. Hash tables are used in hash joins, hash-based aggregation, memoize nodes and hash-based processing of IN subqueries.

Unfortunately, there’s no magic formula for setting work_mem. It depends on your system’s available memory, workload, and query patterns. The [TimescaleDB Team](https://github.com/timescale/timescaledb) has a tool to [autotune](https://github.com/timescale/timescaledb-tune/tree/main) and the topic is widely discussed. Here are some excellent resources to guide you:

- [Everything you know about work_mem is wrong](https://thebuild.com/blog/2023/03/13/everything-you-know-about-setting-work_mem-is-wrong/)
- [How should I tune work_mem for a given system](https://pganalyze.com/blog/5mins-postgres-work-mem-tuning#how-should-i-tune-work_mem-for-a-given-system)

But at the end of the day, IMHO, the answer is: TEST. TEST TODAY. TEST TOMORROW. ~~TEST FOREVER~~. Keep testing until you find an acceptable value for your use case that enhances query performance without blowing up your database. :smile: