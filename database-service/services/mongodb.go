package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type mongoDBBackend struct {
	rootURL string
	host    string
	port    int
}

func newMongoDBBackend(rootURL, host string, port int) *mongoDBBackend {
	return &mongoDBBackend{rootURL: rootURL, host: host, port: port}
}

func (b *mongoDBBackend) connect(ctx context.Context) (*mongo.Client, error) {
	opts := options.Client().
		ApplyURI(b.rootURL).
		SetConnectTimeout(5 * time.Second).
		SetServerSelectionTimeout(5 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return client, nil
}

func (b *mongoDBBackend) ping(ctx context.Context) error {
	client, err := b.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	return client.Ping(ctx, readpref.Primary())
}

// testCredentials verifies that a specific user/password can connect to the named database.
func (b *mongoDBBackend) testCredentials(ctx context.Context, host string, port int, dbName, username, password string) error {
	uri := fmt.Sprintf("mongodb://%s:%s@%s:%d/%s", username, password, host, port, dbName)
	opts := options.Client().
		ApplyURI(uri).
		SetConnectTimeout(5 * time.Second).
		SetServerSelectionTimeout(5 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	return client.Ping(ctx, readpref.Primary())
}

func (b *mongoDBBackend) createDatabase(ctx context.Context, name string) (*DatabaseCredentials, error) {
	dbName := sanitizeIdentifier(name)

	suffix, err := randString(6)
	if err != nil {
		return nil, fmt.Errorf("generate suffix: %w", err)
	}
	username := "u_" + suffix

	password, err := randString(20)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}

	client, err := b.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	// Create the user in the admin database with readWrite access to the target database.
	createUserCmd := bson.D{
		{Key: "createUser", Value: username},
		{Key: "pwd", Value: password},
		{Key: "roles", Value: bson.A{
			bson.D{
				{Key: "role", Value: "readWrite"},
				{Key: "db", Value: dbName},
			},
		}},
	}
	if err := client.Database("admin").RunCommand(ctx, createUserCmd).Err(); err != nil {
		return nil, fmt.Errorf("create mongo user: %w", err)
	}

	// Force the database into existence by creating an init collection.
	if err := client.Database(dbName).CreateCollection(ctx, "_init"); err != nil {
		// Ignore "already exists" errors.
		cmdErr, ok := err.(mongo.CommandError)
		if !ok || cmdErr.Code != 48 { // 48 = NamespaceExists
			return nil, fmt.Errorf("create init collection: %w", err)
		}
	}

	slog.InfoContext(ctx, "mongodb database created", "database", dbName)
	return &DatabaseCredentials{
		DBType:   string(DBTypeMongoDB),
		Host:     b.host,
		Port:     b.port,
		Database: dbName,
		Username: username,
		Password: password,
	}, nil
}
