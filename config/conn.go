package config

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

// var DB *sql.DB
var DB *pgxpool.Pool

func Init() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
}

func InitDB() error {

	// user := os.Getenv("DB_USER")
	// pass := os.Getenv("DB_PASS")
	// host := os.Getenv("DB_HOST")
	// port := os.Getenv("DB_PORT")
	// name := os.Getenv("DB_NAME")

	// dsn := fmt.Sprintf(
	// 	"postgres://%s:%s@%s:%s/%s?sslmode=disable",
	// 	user, url.QueryEscape(pass), host, port, name,
	// )

	// var err error
	// DB, err = sql.Open("pgx", dsn)
	// if err != nil {
	// 	return err
	// }

	// return DB.Ping()

	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASS")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")

	passEscaped := url.QueryEscape(pass)

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, passEscaped, host, port, name,
	)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	DB = pool
	log.Println("Connected to PostgreSQL successfully!")
	return nil
}

func LoadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
}
