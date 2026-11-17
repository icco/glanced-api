package db

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gormtracing "gorm.io/plugin/opentelemetry/tracing"
)

// Open connects to Postgres, migrates the schema, and creates the sync sequence.
func Open(ctx context.Context, url string) (*gorm.DB, error) {
	g, err := gorm.Open(postgres.Open(url), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := g.Use(gormtracing.NewPlugin(gormtracing.WithoutMetrics(), gormtracing.WithoutQueryVariables())); err != nil {
		return nil, fmt.Errorf("use tracing plugin: %w", err)
	}
	g = g.WithContext(ctx)
	if err := g.AutoMigrate(&User{}, &Session{}, &Card{}, &Line{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := g.Exec("CREATE SEQUENCE IF NOT EXISTS glanced_seq").Error; err != nil {
		return nil, fmt.Errorf("create sequence: %w", err)
	}
	return g, nil
}
