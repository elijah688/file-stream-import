// db/db.go
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"import/internal/model"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

func NewDB(ctx context.Context) (*DB, error) {
	connString := "postgres://postgres:pass@localhost:6969/postgres?sslmode=disable"

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	return &DB{
		pool: pool,
	}, nil
}

func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

func (d *DB) ProcessCSVChunks(ctx context.Context, locations []model.Location) error {
	if len(locations) == 0 {
	}

	columns := []string{"locid", "loctimezone", "country", "locname", "business"}
	numColumns := len(columns)
	valueStrings := make([]string, 0, len(locations))
	valueArgs := make([]any, 0, len(locations)*numColumns)

	for i, loc := range locations {
		valuePlaceholder := make([]string, numColumns)
		for j := range columns {
			valuePlaceholder[j] = fmt.Sprintf("$%d", i*numColumns+j+1)
		}
		valueStrings = append(valueStrings, fmt.Sprintf("(%s)", strings.Join(valuePlaceholder, ", ")))

		valueArgs = append(valueArgs, loc.LocID, loc.LocTimeZone, loc.Country, loc.LocName, loc.Business)
	}

	query := fmt.Sprintf(`
		INSERT INTO locations (%s)
		VALUES %s
		ON CONFLICT (locid)
		DO UPDATE SET 
			loctimezone = EXCLUDED.loctimezone,
			country = EXCLUDED.country,
			locname = EXCLUDED.locname,
			business = EXCLUDED.business;
	`, strings.Join(columns, ", "), strings.Join(valueStrings, ", "))

	_, err := d.pool.Exec(ctx, query, valueArgs...)
	if err != nil {
		return fmt.Errorf("failed to batch insert/update locations: %w", err)
	}

	return nil
}

func (d *DB) CreateTableIfNotExist(ctx context.Context) error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS locations (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		locid TEXT UNIQUE,
		loctimezone TEXT,
		country TEXT,
		locname TEXT,
		business TEXT
	);`

	if _, err := d.pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_locid ON locations(locid);
	CREATE INDEX IF NOT EXISTS idx_loctimezone ON locations(loctimezone);
	CREATE INDEX IF NOT EXISTS idx_country ON locations(country);
	CREATE INDEX IF NOT EXISTS idx_locname ON locations(locname);
	CREATE INDEX IF NOT EXISTS idx_business ON locations(business);
	`

	if _, err := d.pool.Exec(ctx, indexSQL); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}
func (d *DB) GetLocations(ctx context.Context, limit, offset int) ([]model.Location, error) {
	query := `
		SELECT COALESCE(json_agg(locations), '[]'::json)
		FROM (
			SELECT * 
			FROM locations 
			LIMIT $1 OFFSET $2
		) AS locations;

	`

	var jsonLocations string

	if err := d.pool.QueryRow(ctx, query, limit, offset).Scan(&jsonLocations); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query locations: %w", err)
	}

	var locations []model.Location
	if err := json.Unmarshal([]byte(jsonLocations), &locations); err != nil {
		return nil, fmt.Errorf("failed to unmarshal locations from JSON: %w", err)
	}

	return locations, nil
}
