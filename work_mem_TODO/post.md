# Briefing

Anos atrás, recebi a missão de investigar e **resolver** a lentidão em um sistema crítico da firma, foram noites mal dormidas e cálvice mais assentuada. O backend em questão utilizava o banco de dados PostgreSQL, e depois de muito suor e investigação o problema foi solucionado em literalmente uma linha:

```sql
ALTER USER foo SET work_mem='32MB';
```

Vou ser sincero, esse conteúdo pode ou não te ajudar de maneira imediata, vai depender muito do padrão das queries do seu sistema, mas definitivamente, se você trabalha com backend, espero que esse conteúdo seja mais uma opção no seu leque para solução de problemas de lentidão, especialmente envolvendo PostgreSQL :smile:.

Ao longo desse post, vamos reproduzir um cenário favorável para a degradação de performance se manifestar e vou te apresentar um conjunto de ferramentas, para investigar minuciosamente o problema, utilizando `EXPLAIN`, k6 para teste de carga e o método científico para debugar o uso de recursos computacionais como, _throughput de disco_, _CPU_ e _Memória_.

# Cenário

Vamos criar um sistema que consiga tirar insights de jogadores e times de futebol dado sua performance em partidas, que consegue responder perguntas do tipo:

- Qual foi o top 10 de jogadores que tiveram o maior número de "minutos jogados" ?
- Quem foi o artilheiro da temporada ?
- Quem foi o lider em assitências ?
- Qual foi o time que mais marcou gols e assitências ?

Para conseguir responder essas perguntas, vamos criar um banco de dados com essas três tabelas:

TODO refactor to remove unuseful things:
```sql
CREATE TABLE players (
    player_id SERIAL PRIMARY KEY,
    name TEXT,
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
```
 
O script que inicializa e popula o banco de dados pode ser visto com detalhes no repositório do github, existem funções customizadas para criar times brasileiros e nomes de jogadores dessa temporada do campeonato brasileiro.

> E sim, poderiamos modelar os dados para que as queries que respondem a essas perguntas sejam mais performáticas e eficientes, porém o intuito aqui é justamente estressar cenários não otimizados. E acreditem, a chance de vc trabalhar em sistemas que te fazem ter que tirar "leite de pedra" não são baixas, seja por erro de modelagem ou por um crescimento inesperado e necessidade de negócio não mapeada.




[] function to randomly generates brazilian soccer team names, maybe some extension for names?
[] deep dive in sorting methods of PostgreSQL, algorithms that used memory vs used disk
[] also mentioning postgresql codebase
[] maybe some discussion related to improvements?

