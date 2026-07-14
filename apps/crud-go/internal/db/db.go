package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/troemmanuel/minipaas/apps/crud-go/internal/models"
)

// ErrNotFound is returned when an item does not exist.
var ErrNotFound = errors.New("item not found")

type Store struct {
	db *sql.DB
}

func Connect(databaseURL string) (*Store, error) {
	conn, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{db: conn}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS items (
			id          SERIAL PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

func (s *Store) Create(ctx context.Context, name, description string) (models.Item, error) {
	var item models.Item
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO items (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at
	`, name, description)
	if err := scanItem(row, &item); err != nil {
		return models.Item{}, err
	}
	return item, nil
}

func (s *Store) List(ctx context.Context) ([]models.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, created_at, updated_at FROM items ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.Item, 0)
	for rows.Next() {
		var item models.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (models.Item, error) {
	var item models.Item
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, created_at, updated_at FROM items WHERE id = $1
	`, id)
	if err := scanItem(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Item{}, ErrNotFound
		}
		return models.Item{}, err
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, id int64, name, description string) (models.Item, error) {
	var item models.Item
	row := s.db.QueryRowContext(ctx, `
		UPDATE items
		SET name = $2, description = $3, updated_at = now()
		WHERE id = $1
		RETURNING id, name, description, created_at, updated_at
	`, id, name, description)
	if err := scanItem(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Item{}, ErrNotFound
		}
		return models.Item{}, err
	}
	return item, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanItem(row *sql.Row, item *models.Item) error {
	return row.Scan(&item.ID, &item.Name, &item.Description, &item.CreatedAt, &item.UpdatedAt)
}
