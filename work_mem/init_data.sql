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
