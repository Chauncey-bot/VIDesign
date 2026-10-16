package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"packaging-ai/internal/agent"
	"packaging-ai/internal/auth"
	"packaging-ai/internal/config"
	"packaging-ai/internal/httpapi"
	"packaging-ai/internal/repository"
	"packaging-ai/internal/taskqueue"
	"packaging-ai/internal/workflow"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.MySQLMaxOpenConns)
	db.SetMaxIdleConns(cfg.MySQLMaxIdleConns)
	db.SetConnMaxLifetime(cfg.MySQLConnMaxLife)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect MySQL: %v", err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect Redis: %v", err)
	}
	defer redisClient.Close()

	llm, err := agent.NewOpenAICompatible(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	if err != nil {
		log.Fatal(err)
	}
	repo := repository.NewMySQL(db)
	if err := repo.EnsureAuthSchema(ctx); err != nil {
		log.Fatalf("ensure auth schema: %v", err)
	}
	authManager, err := auth.NewManager(cfg.AuthSecret, cfg.AuthTTL)
	if err != nil {
		log.Fatal(err)
	}
	flow := workflow.NewIntakeWorkflow(repo, agent.NewLLMIntake(llm))
	image := agent.NewOpenAICompatibleImage(cfg.ImageBaseURL, cfg.ImageAPIKey, cfg.ImageModel)
	design := workflow.NewDesignWorkflow(repo, agent.NewLLMConcept(llm), agent.NewLLMRevision(llm), image, agent.NewLLMFusion(llm))
	queue := taskqueue.NewRedis(asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}, cfg.RedisQueue, cfg.RedisConcurrency, map[string]taskqueue.Handler{
		workflow.ChatTask:     flow.Handle,
		workflow.GenerateTask: design.Handle,
		workflow.RevisionTask: design.Handle,
		workflow.FusionTask:   design.Handle,
	})
	server := httpapi.NewServer(db, queue, authManager)
	defer server.Close()
	log.Printf("packaging-ai listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
