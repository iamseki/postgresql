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
