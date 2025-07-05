package main

import (
	"context"
	"fmt"
	"peekaping/src/config"

	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.uber.org/zap"
)

func NewMongoCommandMonitor(logger *zap.SugaredLogger) *event.CommandMonitor {
	return &event.CommandMonitor{
		Started: func(ctx context.Context, evt *event.CommandStartedEvent) {
			logger.Infow("MongoDB Command Started",
				"command", evt.Command,
				"database", evt.DatabaseName,
				"commandName", evt.CommandName,
				"requestID", evt.RequestID,
			)
		},
		Succeeded: func(ctx context.Context, evt *event.CommandSucceededEvent) {
			logger.Infow("MongoDB Command Succeeded",
				"commandName", evt.CommandName,
				"durationNanos", evt.DurationNanos,
				"reply", evt.Reply,
				"requestID", evt.RequestID,
			)
		},
		Failed: func(ctx context.Context, evt *event.CommandFailedEvent) {
			logger.Errorw("MongoDB Command Failed",
				"commandName", evt.CommandName,
				"durationNanos", evt.DurationNanos,
				"failure", evt.Failure,
				"requestID", evt.RequestID,
			)
		},
	}
}

func ProvideMongoDB(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*mongo.Client, error) {
	DBUri := fmt.Sprintf("mongodb://%s:%s@%s:%s/%s?authSource=admin",
		cfg.DBUser,
		cfg.DBPass,
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBName,
	)
	logger.Infof("Connecting to MongoDB: %s", DBUri)
	ctx := context.Background()

	clientOpts := options.Client().ApplyURI(DBUri)
	// monitor := NewMongoCommandMonitor(logger)
	// clientOpts.SetMonitor(NewMongoCommandMonitor(logger))

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		logger.Error("could not connect to mongodb", err)
		return nil, err
	}
	logger.Info("connected to mongodb")

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		logger.Error("could not ping mongodb", err)
		return nil, err
	}
	return client, nil
}
