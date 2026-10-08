package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/junglegaming/backend-challenge-go/internal/config"
	"github.com/junglegaming/backend-challenge-go/internal/infra/db"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
	transportHttp "github.com/junglegaming/backend-challenge-go/internal/transport/http"
	"github.com/junglegaming/backend-challenge-go/internal/transport/http/handler"
	"github.com/junglegaming/backend-challenge-go/internal/transport/http/middleware"
	"github.com/junglegaming/backend-challenge-go/internal/usecase"
	"github.com/junglegaming/backend-challenge-go/internal/worker"
	"go.uber.org/fx"
)

var ConfigModule = fx.Module("config",
	fx.Provide(config.Load),
)

var LoggerModule = fx.Module("logger",
	fx.Provide(func() *slog.Logger {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}),
)

var DatabaseModule = fx.Module("database",
	fx.Provide(func(lc fx.Lifecycle, cfg config.AppConfig, logger *slog.Logger) (*pgxpool.Pool, error) {
		dbCfg := db.Config{
			Host:     cfg.DBHost,
			Port:     cfg.DBPort,
			User:     cfg.DBUser,
			Password: cfg.DBPassword,
			Database: cfg.DBName,
			SSLMode:  cfg.DBSSLMode,
		}

		pool, err := db.NewPool(context.Background(), dbCfg)
		if err != nil {
			return nil, err
		}

		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				logger.Info("closing postgres connection pool")
				pool.Close()
				return nil
			},
		})

		return pool, nil
	}),
)

var SQSModule = fx.Module("sqs",
	fx.Provide(func(cfg config.AppConfig) (*sqs.Client, error) {
		customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if cfg.AWSEndpoint != "" {
				return aws.Endpoint{
					PartitionID:   "aws",
					URL:           cfg.AWSEndpoint,
					SigningRegion: cfg.AWSRegion,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		})

		awsCfg, err := awsConfig.LoadDefaultConfig(context.Background(),
			awsConfig.WithRegion(cfg.AWSRegion),
			awsConfig.WithEndpointResolverWithOptions(customResolver),
			awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		)
		if err != nil {
			return nil, err
		}

		return sqs.NewFromConfig(awsCfg), nil
	}),
)

var RepositoryModule = fx.Module("repository",
	fx.Provide(repository.NewWalletRepository),
	fx.Provide(repository.NewLedgerRepository),
	fx.Provide(repository.NewTransactionRepository),
	fx.Provide(repository.NewInboxRepository),
	fx.Provide(repository.NewOutboxRepository),
)

var UseCaseModule = fx.Module("usecase",
	fx.Provide(usecase.NewWalletUseCase),
	fx.Provide(usecase.NewWagerUseCase),
)

var AuthModule = fx.Module("auth",
	fx.Provide(func(cfg config.AppConfig) *middleware.AuthMiddleware {
		return middleware.NewAuthMiddleware(cfg.KeycloakIssuer, cfg.JWTSecret, cfg.AllowDevTokens)
	}),
)

var HTTPModule = fx.Module("http",
	fx.Provide(handler.NewWalletHandler),
	fx.Provide(handler.NewWagerHandler),
	fx.Provide(handler.NewHealthHandler),
	fx.Provide(transportHttp.NewRouter),
	fx.Provide(func(handler http.Handler, cfg config.AppConfig) *http.Server {
		return &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Port),
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
	}),
	fx.Invoke(func(lc fx.Lifecycle, server *http.Server, logger *slog.Logger) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				ln, err := net.Listen("tcp", server.Addr)
				if err != nil {
					return err
				}
				logger.Info("HTTP server starting", "addr", server.Addr)
				go func() {
					if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
						logger.Error("HTTP server failed", "error", err)
					}
				}()
				return nil
			},
			OnStop: func(ctx context.Context) error {
				logger.Info("stopping HTTP server gracefully")
				return server.Shutdown(ctx)
			},
		})
	}),
)

var WorkerModule = fx.Module("workers",
	fx.Provide(func(logger *slog.Logger, outboxRepo *repository.OutboxRepository, sqsClient *sqs.Client, cfg config.AppConfig) *worker.OutboxPublisher {
		return worker.NewOutboxPublisher(logger, outboxRepo, sqsClient, cfg.SQSOutboxQueueURL)
	}),
	fx.Provide(worker.NewPendingReferenceResolver),
	fx.Provide(func(logger *slog.Logger, pool *pgxpool.Pool, inboxRepo *repository.InboxRepository, wagerUC *usecase.WagerUseCase, sqsClient *sqs.Client, cfg config.AppConfig) *worker.SQSConsumer {
		return worker.NewSQSConsumer(logger, pool, inboxRepo, wagerUC, sqsClient, cfg.SQSCommandQueueURL)
	}),
	fx.Invoke(func(
		lc fx.Lifecycle,
		publisher *worker.OutboxPublisher,
		resolver *worker.PendingReferenceResolver,
		consumer *worker.SQSConsumer,
	) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				publisher.Start()
				resolver.Start()
				consumer.Start()
				return nil
			},
			OnStop: func(ctx context.Context) error {
				_ = consumer.Stop(ctx)
				_ = resolver.Stop(ctx)
				_ = publisher.Stop(ctx)
				return nil
			},
		})
	}),
)

func main() {
	app := fx.New(
		ConfigModule,
		LoggerModule,
		DatabaseModule,
		SQSModule,
		RepositoryModule,
		UseCaseModule,
		AuthModule,
		HTTPModule,
		WorkerModule,
	)

	app.Run()
}
